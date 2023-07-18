package service

import (
	"net/http"

	"github.com/keikoproj/lifecycle-manager/pkg/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// MetricsNamespace is the namespace of prometheus metrics
	MetricsNamespace = "lifecycle_manager"
	// MetricsPort is the port used to serve metrics
	MetricsPort = ":8080"
	// MetricsEndpoint is the endpoint to expose for metrics
	MetricsEndpoint = "/metrics"
)

const (
	ActiveGoroutinesMetric            = "active_goroutines"
	TerminatingInstancesCountMetric   = "terminating_instances_count"
	DrainingInstancesCountMetric      = "draining_instances_count"
	DeregisteringInstancesCountMetric = "deregistering_instances_count"
	AverageDurationSecondsMetric      = "average_duration_seconds"
	SuccessfulEventsTotalMetric       = "successful_events_total"
	SuccessfulLBDeregisterTotalMetric = "successful_lb_deregister_total"
	SuccessfulNodeDrainTotalMetric    = "successful_node_drain_total"
	SuccessfulNodeDeleteTotalMetric   = "successful_node_delete_total"
	FailedEventsTotalMetric           = "failed_events_total"
	FailedLBDeregisterTotalMetric     = "failed_lb_deregister_total"
	FailedNodeDrainTotalMetric        = "failed_node_drain_total"
	FailedNodeDeleteTotalMetric       = "failed_node_delete_total"
	RejectedEventsTotalMetric         = "rejected_events_total"
)

type MetricsServer struct {
	Counters map[string]prometheus.Counter
	Gauges   map[string]prometheus.Gauge
}

func (m *MetricsServer) Start() {
	m.Gauges = make(map[string]prometheus.Gauge, 0)
	m.Counters = make(map[string]prometheus.Counter, 0)

	gaugeIndex := map[string]string{
		ActiveGoroutinesMetric:            "indicates the current number of active goroutines.",
		TerminatingInstancesCountMetric:   "indicates the current number of terminating instances.",
		DrainingInstancesCountMetric:      "indicates the current number of draining instances.",
		DeregisteringInstancesCountMetric: "indicates the current number of deregistering instances.",
		AverageDurationSecondsMetric:      "indicates the average duration of processing a hook in seconds.",
	}

	counterIndex := map[string]string{
		SuccessfulEventsTotalMetric:       "indicates the sum of all successful events.",
		SuccessfulLBDeregisterTotalMetric: "indicates the sum of all events that succeeded to deregister loadbalancer",
		SuccessfulNodeDrainTotalMetric:    "indicates the sum of all events that succeeded to drain the node.",
		SuccessfulNodeDeleteTotalMetric:   "indicates the sum of all events that succeeded to delete the node.",
		FailedEventsTotalMetric:           "indicates the sum of all failed events.",
		FailedLBDeregisterTotalMetric:     "indicates the sum of all events that failed to deregister loadbalancer.",
		FailedNodeDrainTotalMetric:        "indicates the sum of all events that failed to drain the node.",
		FailedNodeDeleteTotalMetric:       "indicates the sum of all events that failed to delete the node.",
		RejectedEventsTotalMetric:         "indicates the sum of all rejected events.",
	}

	for gaugeName, desc := range gaugeIndex {
		gauge := prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: MetricsNamespace,
				Name:      string(gaugeName),
				Help:      desc,
			})
		m.Gauges[string(gaugeName)] = gauge
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

func (m *MetricsServer) AddCounter(idx string, value float64) {
	if val, ok := m.Counters[idx]; ok {
		val.Add(value)
	}
}

func (m *MetricsServer) SetGauge(idx string, value float64) {
	if val, ok := m.Gauges[idx]; ok {
		val.Set(value)
	}
}

func (m *MetricsServer) IncGauge(idx string) {
	if val, ok := m.Gauges[idx]; ok {
		val.Inc()
	}
}

func (m *MetricsServer) DecGauge(idx string) {
	if val, ok := m.Gauges[idx]; ok {
		val.Dec()
	}
}
