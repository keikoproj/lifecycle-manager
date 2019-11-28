package service

import (
	"net/http"

	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// MetricsNamespace is the namespace of prometheus metrics
	MetricsNamespace = "process"
	// MetricsPort is the port used to serve metrics
	MetricsPort = ":8080"
	// MetricsEndpoint is the endpoint to expose for metrics
	MetricsEndpoint = "/metrics"
)

func (m *MetricsServer) Start() {
	m.Gauges = make(map[string]prometheus.Gauge, 0)
	m.Counters = make(map[string]prometheus.Counter, 0)

	gaugeIndex := map[string]string{
		"active_goroutines":             "indicates the current number of active goroutines.",
		"terminating_instances_count":   "indicates the current number of terminating instances.",
		"draining_instances_count":      "indicates the current number of draining instances.",
		"deregistering_instances_count": "indicates the current number of deregistering instances.",
		"average_duration_seconds":      "indicates the average duration of processing a hook in seconds.",
	}

	counterIndex := map[string]string{
		"successful_events_total":        "indicates the sum of all successful events.",
		"successful_lb_deregister_total": "indicates the sum of all events that succeeded to deregister loadbalancer",
		"successful_node_drain_total":    "indicates the sum of all events that succeeded to drain the node.",
		"failed_events_total":            "indicates the sum of all failed events.",
		"failed_lb_deregister_total":     "indicates the sum of all events that failed to deregister loadbalancer.",
		"failed_node_drain_total":        "indicates the sum of all events that failed to drain the node.",
		"rejected_events_total":          "indicates the sum of all rejected events.",
	}

	for gaugeName, desc := range gaugeIndex {
		gauge := prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: MetricsNamespace,
				Name:      gaugeName,
				Help:      desc,
			})
		m.Gauges[gaugeName] = gauge
	}

	for counterName, desc := range counterIndex {
		counter := prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: MetricsNamespace,
				Name:      counterName,
				Help:      desc,
			},
		)
		m.Counters[counterName] = counter
	}

	http.Handle(MetricsEndpoint, promhttp.Handler())

	for _, gauge := range m.Gauges {
		prometheus.MustRegister(gauge)
	}

	for _, counter := range m.Counters {
		prometheus.MustRegister(counter)
	}

	log.Fatal(http.ListenAndServe(MetricsPort, nil))
}
