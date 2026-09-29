// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

// Package exporter implements the etcd-metrics-exporter sidecar logic: a persistent etcd watch that
// counts resource events and a periodic reader that exposes the configured quota-backend-bytes, both
// surfaced as Prometheus metrics over an HTTP endpoint.
package exporter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	// dialTimeout is the timeout used when establishing the etcd client connection.
	dialTimeout = 10 * time.Second
	// configRefreshInterval is how often the etcd config file is re-read to pick up ConfigMap updates.
	configRefreshInterval = 30 * time.Second
	// metricsPath is the HTTP path on which Prometheus metrics are served.
	metricsPath = "/metrics"
)

// Config holds the runtime configuration for the exporter, populated from command line flags.
type Config struct {
	// Endpoint is the etcd endpoint to connect to.
	Endpoint string
	// CACertPath is the path to the CA bundle used to verify the etcd server certificate.
	CACertPath string
	// CertPath is the path to the client TLS certificate.
	CertPath string
	// KeyPath is the path to the client TLS private key.
	KeyPath string
	// InsecureTransport disables TLS entirely (plain HTTP) when true.
	InsecureTransport bool
	// InsecureSkipTLSVerify disables server certificate verification when true.
	InsecureSkipTLSVerify bool
	// ConfigFile is the path to the mounted etcd configuration file.
	ConfigFile string
	// MetricsPort is the port on which the /metrics endpoint is served.
	MetricsPort int
	// WatchReconnectBackoff is the base backoff between watch reconnection attempts.
	WatchReconnectBackoff time.Duration
}

// Exporter wires together the etcd client, metrics collectors and HTTP server.
type Exporter struct {
	cfg     Config
	metrics *metrics
}

// New constructs an Exporter from the supplied configuration.
func New(cfg Config) *Exporter {
	return &Exporter{
		cfg:     cfg,
		metrics: newMetrics(),
	}
}

// buildTLSConfig assembles a *tls.Config from the exporter configuration. It returns nil (no TLS)
// when insecure transport is requested.
func buildTLSConfig(cfg Config) (*tls.Config, error) {
	if cfg.InsecureTransport {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipTLSVerify,
	}

	// Load the client certificate/key pair for mutual TLS when both are provided.
	if cfg.CertPath != "" && cfg.KeyPath != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load client cert/key pair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load the CA bundle to verify the etcd server certificate when provided.
	if cfg.CACertPath != "" {
		caData, err := os.ReadFile(cfg.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert file %q: %w", cfg.CACertPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to parse any CA certificates from %q", cfg.CACertPath)
		}
		tlsConfig.RootCAs = pool
	}

	return tlsConfig, nil
}

// newEtcdClient constructs an etcd v3 client from the exporter configuration.
func newEtcdClient(cfg Config) (*clientv3.Client, error) {
	tlsConfig, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	return clientv3.New(clientv3.Config{
		Endpoints:   []string{cfg.Endpoint},
		TLS:         tlsConfig,
		DialTimeout: dialTimeout,
	})
}

// refreshQuota reads the etcd config file once and updates the configured-quota gauge accordingly.
// Missing fields and read/parse errors are logged as warnings and do not propagate, so a transient
// config problem never crashes the exporter.
func (e *Exporter) refreshQuota() {
	quota, present, err := parseQuotaBackendBytes(e.cfg.ConfigFile)
	if err != nil {
		log.Printf("warning: unable to read quota-backend-bytes from %q: %v", e.cfg.ConfigFile, err)
		return
	}
	if !present {
		log.Printf("warning: quota-backend-bytes not found in %q; leaving gauge unset", e.cfg.ConfigFile)
		return
	}
	e.metrics.configuredQuota.Set(float64(quota))
}

// runConfigRefreshLoop performs an initial quota read and then re-reads the config file periodically
// until the context is cancelled.
func (e *Exporter) runConfigRefreshLoop(ctx context.Context) {
	e.refreshQuota()
	ticker := time.NewTicker(configRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.refreshQuota()
		}
	}
}

// Run starts the config-refresh loop, the etcd watch loop and the metrics HTTP server, blocking until
// the supplied context is cancelled and all components have shut down.
func (e *Exporter) Run(ctx context.Context) error {
	cli, err := newEtcdClient(e.cfg)
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			log.Printf("error closing etcd client: %v", cerr)
		}
	}()

	var wg sync.WaitGroup

	// Config-refresh loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		e.runConfigRefreshLoop(ctx)
	}()

	// Watch loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		runWatch(ctx, cli, e.metrics, e.cfg.WatchReconnectBackoff)
	}()

	// HTTP server for the metrics endpoint.
	mux := http.NewServeMux()
	mux.Handle(metricsPath, promhttp.HandlerFor(e.metrics.registry, promhttp.HandlerOpts{}))
	server := &http.Server{
		Addr:              ":" + strconv.Itoa(e.cfg.MetricsPort),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		log.Printf("serving metrics on %s%s", server.Addr, metricsPath)
		if serveErr := server.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			serverErrCh <- serveErr
			return
		}
		serverErrCh <- nil
	}()

	// Wait for either shutdown signal (ctx cancelled) or a fatal server error.
	var runErr error
	select {
	case <-ctx.Done():
		log.Printf("shutdown requested; stopping exporter")
	case serveErr := <-serverErrCh:
		if serveErr != nil {
			runErr = fmt.Errorf("metrics server failed: %w", serveErr)
		}
	}

	// Gracefully shut down the HTTP server.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		log.Printf("error shutting down metrics server: %v", shutdownErr)
	}

	// The background goroutines observe ctx cancellation; wait for them to finish.
	wg.Wait()
	return runErr
}
