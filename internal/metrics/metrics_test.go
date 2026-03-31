package metrics

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestRecordHTTPRequest(t *testing.T) {
	labels200 := map[string]string{"method": "GET", "handler": "/nodes", "status": "200"}
	labels404 := map[string]string{"method": "GET", "handler": "/nodes", "status": "404"}

	// Snapshot before: counters accumulate across test runs with -count=N.
	before200 := findCounterValueFromRegistry(labels200, "cetacean_http_requests_total")
	before404 := findCounterValueFromRegistry(labels404, "cetacean_http_requests_total")

	RecordHTTPRequest("/nodes", "GET", 200, 0.05, 0, 512)
	RecordHTTPRequest("/nodes", "GET", 200, 0.10, 0, 1024)
	RecordHTTPRequest("/nodes", "GET", 404, 0.01, 0, 64)

	after200 := findCounterValueFromRegistry(labels200, "cetacean_http_requests_total")
	after404 := findCounterValueFromRegistry(labels404, "cetacean_http_requests_total")

	if delta := after200 - before200; delta != 2 {
		t.Errorf("expected 2 new requests with status 200, got %v", delta)
	}

	if delta := after404 - before404; delta != 1 {
		t.Errorf("expected 1 new request with status 404, got %v", delta)
	}
}

func gatherMetric(t *testing.T, name string) *dto.MetricFamily {
	t.Helper()

	metrics, err := Registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	family := findMetricFamily(metrics, name)
	if family == nil {
		t.Fatalf("%s not found", name)
	}

	return family
}

func TestRecordSSEConnect(t *testing.T) {
	RecordSSEConnect()
	family := gatherMetric(t, "cetacean_sse_connections_active")

	value := family.GetMetric()[0].GetGauge().GetValue()
	if value < 1 {
		t.Errorf("expected active connections >= 1, got %v", value)
	}
}

func TestRecordSSEDisconnect(t *testing.T) {
	RecordSSEConnect()
	RecordSSEDisconnect()
	gatherMetric(t, "cetacean_sse_connections_active")
}

func TestRecordSSEBroadcast(t *testing.T) {
	RecordSSEBroadcast()
	family := gatherMetric(t, "cetacean_sse_events_broadcast_total")

	value := family.GetMetric()[0].GetCounter().GetValue()
	if value < 1 {
		t.Errorf("expected broadcast count >= 1, got %v", value)
	}
}

func TestRecordSSEDrop(t *testing.T) {
	RecordSSEDrop()
	family := gatherMetric(t, "cetacean_sse_events_dropped_total")

	value := family.GetMetric()[0].GetCounter().GetValue()
	if value < 1 {
		t.Errorf("expected dropped count >= 1, got %v", value)
	}
}

func TestSetCacheResources(t *testing.T) {
	SetCacheResources("services", 42)
	family := gatherMetric(t, "cetacean_cache_resources")

	value := findGaugeValue(family, map[string]string{"type": "services"})
	if value != 42 {
		t.Errorf("expected 42, got %v", value)
	}
}

func TestObserveSyncDuration(t *testing.T) {
	ObserveSyncDuration(1.5)
	family := gatherMetric(t, "cetacean_cache_sync_duration_seconds")

	count := family.GetMetric()[0].GetHistogram().GetSampleCount()
	if count < 1 {
		t.Errorf("expected sample count >= 1, got %v", count)
	}
}

func TestRecordCacheMutation(t *testing.T) {
	RecordCacheMutation("services", "set")
	family := gatherMetric(t, "cetacean_cache_mutations_total")

	value := findCounterValue(family, map[string]string{"type": "services", "action": "set"})
	if value < 1 {
		t.Errorf("expected mutation count >= 1, got %v", value)
	}
}

func TestRecordPrometheusRequest(t *testing.T) {
	RecordPrometheusRequest(200, 0.5)
	family := gatherMetric(t, "cetacean_prometheus_requests_total")

	value := findCounterValue(family, map[string]string{"status": "200"})
	if value < 1 {
		t.Errorf("expected request count >= 1, got %v", value)
	}

	duration := gatherMetric(t, "cetacean_prometheus_request_duration_seconds")

	count := duration.GetMetric()[0].GetHistogram().GetSampleCount()
	if count < 1 {
		t.Errorf("expected sample count >= 1, got %v", count)
	}
}

func TestObserveRecommendationCheck(t *testing.T) {
	ObserveRecommendationCheck("sizing", 0.3)
	family := gatherMetric(t, "cetacean_recommendations_check_duration_seconds")

	if len(family.GetMetric()) == 0 {
		t.Fatal("expected at least one metric")
	}
}

func TestSetRecommendationCounts(t *testing.T) {
	SetRecommendationCounts(1, 5, 10)
	family := gatherMetric(t, "cetacean_recommendations_total")

	critical := findGaugeValue(family, map[string]string{"severity": "critical"})
	warning := findGaugeValue(family, map[string]string{"severity": "warning"})
	info := findGaugeValue(family, map[string]string{"severity": "info"})

	if critical != 1 {
		t.Errorf("expected critical=1, got %v", critical)
	}

	if warning != 5 {
		t.Errorf("expected warning=5, got %v", warning)
	}

	if info != 10 {
		t.Errorf("expected info=10, got %v", info)
	}
}

func findGaugeValue(family *dto.MetricFamily, labels map[string]string) float64 {
	for _, metric := range family.GetMetric() {
		if matchLabels(metric.GetLabel(), labels) {
			return metric.GetGauge().GetValue()
		}
	}

	return 0
}

func findMetricFamily(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}

	return nil
}

func findCounterValue(family *dto.MetricFamily, labels map[string]string) float64 {
	for _, metric := range family.GetMetric() {
		if matchLabels(metric.GetLabel(), labels) {
			return metric.GetCounter().GetValue()
		}
	}

	return 0
}

func findCounterValueFromRegistry(labels map[string]string, name string) float64 {
	families, _ := Registry.Gather()
	family := findMetricFamily(families, name)
	if family == nil {
		return 0
	}

	return findCounterValue(family, labels)
}

func matchLabels(pairs []*dto.LabelPair, expected map[string]string) bool {
	if len(pairs) != len(expected) {
		return false
	}

	for _, pair := range pairs {
		value, ok := expected[pair.GetName()]
		if !ok || value != pair.GetValue() {
			return false
		}
	}

	return true
}
