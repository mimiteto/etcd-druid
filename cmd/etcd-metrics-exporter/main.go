// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gardener/etcd-druid/internal/exporter"

	flag "github.com/spf13/pflag"
)

func main() {
	cfg := exporter.Config{}

	flag.StringVar(&cfg.Endpoint, "endpoint", "https://127.0.0.1:2379", "etcd endpoint to connect to")
	flag.StringVar(&cfg.CACertPath, "cacert", "", "path to the CA bundle used to verify the etcd server certificate")
	flag.StringVar(&cfg.CertPath, "cert", "", "path to the client TLS certificate")
	flag.StringVar(&cfg.KeyPath, "key", "", "path to the client TLS private key")
	flag.BoolVar(&cfg.InsecureTransport, "insecure-transport", false, "use plain HTTP and no TLS client configuration")
	flag.BoolVar(&cfg.InsecureSkipTLSVerify, "insecure-skip-tls-verify", false, "skip verification of the etcd server certificate")
	flag.StringVar(&cfg.ConfigFile, "config-file", "/var/etcd/config/etcd.conf.yaml", "path to the mounted etcd configuration file")
	flag.IntVar(&cfg.MetricsPort, "metrics-port", 9096, "port on which the /metrics endpoint is served")
	flag.DurationVar(&cfg.WatchReconnectBackoff, "watch-reconnect-backoff", 5*time.Second, "base backoff between etcd watch reconnection attempts")
	flag.Parse()

	// Cancel the root context on SIGTERM/SIGINT for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	exp := exporter.New(cfg)
	log.Printf("starting etcd-metrics-exporter (endpoint=%s, config-file=%s, metrics-port=%d)", cfg.Endpoint, cfg.ConfigFile, cfg.MetricsPort)
	if err := exp.Run(ctx); err != nil {
		log.Printf("etcd-metrics-exporter exited with error: %v", err)
		os.Exit(1)
	}
	log.Printf("etcd-metrics-exporter stopped")
}
