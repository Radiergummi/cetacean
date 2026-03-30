package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestRecordHTTPRequest(t *testing.T) {
	RecordHTTPRequest("/nodes", "GET", 200, 0.05, 0, 512)
	RecordHTTPRequest("/nodes", "GET", 200, 0.10, 0, 1024)
	RecordHTTPRequest("/nodes", "GET", 404, 0.01, 0, 64)

	metrics, err := Registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	counter := findMetricFamily(metrics, "cetacean_http_requests_total")
	if counter == nil {
		t.Fatal("cetacean_http_requests_total not found")
	}

	got200 := findCounterValue(counter, map[string]string{
		"method":  "GET",
		"handler": "/nodes",
		"status":  "200",
	})

	if got200 != 2 {
		t.Errorf("expected 2 requests with status 200, got %v", got200)
	}

	got404 := findCounterValue(counter, map[string]string{
		"method":  "GET",
		"handler": "/nodes",
		"status":  "404",
	})

	if got404 != 1 {
		t.Errorf("expected 1 request with status 404, got %v", got404)
	}
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

func init() {
	// Ensure the registry satisfies the Gatherer interface at compile time.
	var _ prometheus.Gatherer = Registry
}
