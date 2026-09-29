// SPDX-FileCopyrightText: Contributors to the Gardener project
//
// SPDX-License-Identifier: Apache-2.0

package exporter

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// namespaceEtcdDruid is the Prometheus namespace shared by etcd-druid metrics.
	namespaceEtcdDruid = "etcddruid"
	// subsystemExporter is the Prometheus subsystem for the etcd-metrics-exporter sidecar.
	subsystemExporter = "exporter"

	// labelResource is the metric label holding the etcd resource kind (e.g. leases, pods).
	labelResource = "resource"
	// labelEventType is the metric label holding the event type (PUT or DELETE).
	labelEventType = "event_type"
)

// metrics bundles the Prometheus collectors exposed by the exporter and the registry they are registered with.
type metrics struct {
	registry *prometheus.Registry
	// resourceEvents counts observed etcd events, partitioned by resource kind and event type.
	resourceEvents *prometheus.CounterVec
	// configuredQuota exposes the configured quota-backend-bytes value read from the etcd config file.
	configuredQuota prometheus.Gauge
}

// newMetrics constructs the exporter metrics and registers them with a dedicated Prometheus registry.
func newMetrics() *metrics {
	registry := prometheus.NewRegistry()
	m := &metrics{
		registry: registry,
		resourceEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespaceEtcdDruid,
			Subsystem: subsystemExporter,
			Name:      "resource_events_total",
			Help:      "Total number of etcd resource events observed via the watch, partitioned by resource kind and event type.",
		}, []string{labelResource, labelEventType}),
		configuredQuota: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespaceEtcdDruid,
			Subsystem: subsystemExporter,
			Name:      "configured_quota_backend_bytes",
			Help:      "The configured quota-backend-bytes value read from the mounted etcd configuration file.",
		}),
	}
	registry.MustRegister(m.resourceEvents, m.configuredQuota)
	return m
}
