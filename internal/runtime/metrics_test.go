package runtime

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/tunely-eu/bifrost"
)

func TestCaddyObserverRecordsEndpointMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	observer, err := NewCaddyObserver(registry)
	if err != nil {
		t.Fatalf("NewCaddyObserver: %v", err)
	}

	observer.SessionStarted("home")
	stream := observer.StreamStarted("home")
	stream.AddBytes(bifrost.DirectionIngressToEndpoint, 10)
	stream.AddBytes(bifrost.DirectionEndpointToIngress, 20)
	stream.End()
	observer.StreamRejected("home", "stream_limit")
	observer.SessionEnded("home")

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	assertMetricValue(t, families, "bifrost_endpoint_active_sessions", map[string]string{"endpoint_key": "home"}, 0)
	assertMetricValue(t, families, "bifrost_endpoint_streams_started_total", map[string]string{"endpoint_key": "home"}, 1)
	assertMetricValue(t, families, "bifrost_endpoint_streams_ended_total", map[string]string{"endpoint_key": "home"}, 1)
	assertMetricValue(t, families, "bifrost_endpoint_streams_rejected_total", map[string]string{
		"endpoint_key": "home",
		"reason":       "stream_limit",
	}, 1)
	assertMetricValue(t, families, "bifrost_endpoint_stream_bytes_total", map[string]string{
		"endpoint_key": "home",
		"direction":    string(bifrost.DirectionIngressToEndpoint),
	}, 10)
	assertMetricValue(t, families, "bifrost_endpoint_stream_bytes_total", map[string]string{
		"endpoint_key": "home",
		"direction":    string(bifrost.DirectionEndpointToIngress),
	}, 20)
}

func TestCaddyObserverReusesRegisteredCollectors(t *testing.T) {
	registry := prometheus.NewRegistry()
	first, err := NewCaddyObserver(registry)
	if err != nil {
		t.Fatalf("first observer: %v", err)
	}
	second, err := NewCaddyObserver(registry)
	if err != nil {
		t.Fatalf("second observer: %v", err)
	}
	first.SessionStarted("home")
	second.SessionStarted("home")

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	assertMetricValue(t, families, "bifrost_endpoint_active_sessions", map[string]string{"endpoint_key": "home"}, 2)
}

func assertMetricValue(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string, value float64) {
	t.Helper()
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if !metricLabelsEqual(metric, labels) {
				continue
			}
			if metric.Gauge != nil && metric.Gauge.GetValue() == value {
				return
			}
			if metric.Counter != nil && metric.Counter.GetValue() == value {
				return
			}
			t.Fatalf("%s%v = %#v, want %v", name, labels, metric, value)
		}
	}
	t.Fatalf("missing metric %s%v", name, labels)
}

func metricLabelsEqual(metric *dto.Metric, labels map[string]string) bool {
	if len(metric.GetLabel()) != len(labels) {
		return false
	}
	for _, label := range metric.GetLabel() {
		if labels[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}
