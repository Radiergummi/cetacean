package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/prom"
)

// fakeQuerier records what get_metrics asked Prometheus for, which is the half
// of this tool worth pinning: the queries are Cetacean's, so what they select
// on is the contract, not the numbers that come back.
type fakeQuerier struct {
	queries []string
	starts  []string
	ends    []string
	steps   []string
	series  []prom.Series
	err     error
}

func (f *fakeQuerier) RangeQuery(
	_ context.Context,
	query, start, end, step string,
) ([]prom.Series, error) {
	f.queries = append(f.queries, query)
	f.starts = append(f.starts, start)
	f.ends = append(f.ends, end)
	f.steps = append(f.steps, step)

	return f.series, f.err
}

func seedMetricsCache(t *testing.T) *cache.Cache {
	t.Helper()

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "monitoring_prometheus"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "secret-app"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
		Status:      swarm.NodeStatus{Addr: "10.0.0.7"},
	})
	c.SetNode(swarm.Node{
		ID:          "node2",
		Description: swarm.NodeDescription{Hostname: "worker-2"},
	})

	return c
}

func metricsServer(t *testing.T, querier MetricsQuerier, overrides ...func(*Options)) *Server {
	t.Helper()

	options := append([]func(*Options){
		func(o *Options) { o.Prometheus = querier },
	}, overrides...)

	return newToolTestServer(
		t,
		seedMetricsCache(t),
		&fakeWriteClient{},
		config.OpsReadOnly,
		options...,
	)
}

func callGetMetrics(t *testing.T, srv *Server, args map[string]any) (metricsResult, error) {
	t.Helper()

	td, ok := srv.findTool("get_metrics")
	if !ok {
		t.Fatal("get_metrics is not registered at OpsReadOnly")
	}

	text, err := td.handler(ctxWithIdentity(), newCallToolRequest("get_metrics", args))
	if err != nil {
		return metricsResult{}, err
	}

	var result metricsResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("decode result: %v: %s", err, text)
	}

	return result, nil
}

// The id an agent passes is a handle, not a selector: it is resolved against
// the cache and the *cached name* is what reaches PromQL. Without that, a
// caller could pass a label expression as the id and query anything.
func TestGetMetricsSelectsOnTheCachedServiceName(t *testing.T) {
	querier := &fakeQuerier{}
	srv := metricsServer(t, querier)

	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "service",
		"id":     "svc1",
		"metric": "cpu",
	}); err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	if len(querier.queries) != 1 {
		t.Fatalf("ran %d queries, want 1: %v", len(querier.queries), querier.queries)
	}

	query := querier.queries[0]
	if !strings.Contains(query, `container_label_com_docker_swarm_service_name="monitoring_prometheus"`) {
		t.Errorf("query does not select on the cached service name: %s", query)
	}

	if strings.Contains(query, "svc1") {
		t.Errorf("query selects on the id the caller passed rather than the resolved name: %s", query)
	}
}

func TestGetMetricsAcceptsAServiceNameAsWellAsAnID(t *testing.T) {
	querier := &fakeQuerier{}
	srv := metricsServer(t, querier)

	result, err := callGetMetrics(t, srv, map[string]any{
		"target": "service",
		"id":     "monitoring_prometheus",
	})
	if err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	if result.Name != "monitoring_prometheus" {
		t.Errorf("name = %q, want monitoring_prometheus", result.Name)
	}
}

// A denied resource must not be measurable either: metrics leak the shape of a
// workload — when it was busy, when it restarted — to a caller who cannot see
// the service itself.
func TestGetMetricsAppliesACL(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:monitoring_*"))

	querier := &fakeQuerier{}
	srv := metricsServer(t, querier, func(o *Options) { o.ACL = evaluator })

	_, err := callGetMetrics(t, srv, map[string]any{"target": "service", "id": "svc2"})
	if err == nil {
		t.Fatal("expected a denial for a service the caller may not read")
	}

	if len(querier.queries) != 0 {
		t.Errorf("queried Prometheus for a denied service: %v", querier.queries)
	}
}

func TestGetMetricsMatchesANodeByItsAddressThenItsHostname(t *testing.T) {
	querier := &fakeQuerier{}
	srv := metricsServer(t, querier)

	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "node",
		"id":     "node1",
		"metric": "memory",
	}); err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	if !strings.Contains(querier.queries[0], `instance=~"10.0.0.7:.*"`) {
		t.Errorf("node with an address should match on it: %s", querier.queries[0])
	}

	// node2 has no address, so the hostname is all there is to match on.
	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "node",
		"id":     "worker-2",
		"metric": "cpu",
	}); err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	if !strings.Contains(querier.queries[1], `instance=~"worker-2`) {
		t.Errorf("node without an address should match on its hostname: %s", querier.queries[1])
	}
}

func TestGetMetricsReturnsBothNetworkDirections(t *testing.T) {
	querier := &fakeQuerier{
		series: []prom.Series{{
			Points: []prom.Point{{Timestamp: 1788254400, Value: 1024}},
		}},
	}
	srv := metricsServer(t, querier)

	result, err := callGetMetrics(t, srv, map[string]any{
		"target": "service",
		"id":     "svc1",
		"metric": "network",
	})
	if err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	if len(result.Series) != 2 {
		t.Fatalf("got %d series, want receive and transmit: %+v", len(result.Series), result.Series)
	}

	if result.Series[0].Name != "receive" || result.Series[1].Name != "transmit" {
		t.Errorf("series names = %q/%q, want receive/transmit",
			result.Series[0].Name, result.Series[1].Name)
	}

	if result.Unit != "bytes/s" {
		t.Errorf("unit = %q, want bytes/s", result.Unit)
	}

	// Timestamps are formatted for a reader, not left as epoch seconds.
	if got := result.Series[0].Points[0].Time; !strings.HasSuffix(got, "Z") {
		t.Errorf("point time = %q, want an RFC 3339 timestamp", got)
	}
}

func TestGetMetricsResolutionFollowsTheRange(t *testing.T) {
	querier := &fakeQuerier{}
	srv := metricsServer(t, querier)

	for _, tc := range []struct{ rangeKey, step string }{
		{"1h", "60s"},
		{"7d", "3600s"},
	} {
		if _, err := callGetMetrics(t, srv, map[string]any{
			"target": "service",
			"id":     "svc1",
			"range":  tc.rangeKey,
		}); err != nil {
			t.Fatalf("get_metrics %s: %v", tc.rangeKey, err)
		}

		if got := querier.steps[len(querier.steps)-1]; got != tc.step {
			t.Errorf("range %s: step = %q, want %q", tc.rangeKey, got, tc.step)
		}
	}
}

// A Cetacean without Prometheus has nothing to chart, and saying so is more
// use to an agent than an empty series it would read as "idle".
func TestGetMetricsSaysWhenPrometheusIsUnconfigured(t *testing.T) {
	srv := metricsServer(t, nil)

	_, err := callGetMetrics(t, srv, map[string]any{"target": "service", "id": "svc1"})
	if err == nil {
		t.Fatal("expected an error when no Prometheus is configured")
	}

	if !strings.Contains(err.Error(), "CETACEAN_PROMETHEUS_URL") {
		t.Errorf("error = %q, want it to name the setting that enables metrics", err)
	}
}

func TestGetMetricsRejectsUnknownArguments(t *testing.T) {
	querier := &fakeQuerier{}
	srv := metricsServer(t, querier)

	for name, args := range map[string]map[string]any{
		"target": {"target": "stack", "id": "svc1"},
		"metric": {"target": "service", "id": "svc1", "metric": "iops"},
		"range":  {"target": "service", "id": "svc1", "range": "30d"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := callGetMetrics(t, srv, args); err == nil {
				t.Errorf("expected an error for an unknown %s", name)
			}
		})
	}

	if len(querier.queries) != 0 {
		t.Errorf("a rejected request still queried Prometheus: %v", querier.queries)
	}
}

// TestGetMetricsPointsAtTheMetricsWidget — the tool half of the MCP Apps
// contract; see TestGetLogsPointsAtTheLogWidget.
func TestGetMetricsPointsAtTheMetricsWidget(t *testing.T) {
	srv := metricsServer(t, &fakeQuerier{})

	td, ok := srv.findTool("get_metrics")
	if !ok {
		t.Fatal("get_metrics is not registered at OpsReadOnly")
	}

	if td.widget != "metrics" {
		t.Errorf("widget = %q, want metrics", td.widget)
	}

	if td.tool.Meta == nil {
		t.Fatal("get_metrics carries no _meta; hosts cannot find its widget")
	}

	if got := td.tool.Meta.AdditionalFields[uiResourceURIMetaKey]; got != uiResourceURI("metrics") {
		t.Errorf("_meta[%q] = %v, want %q", uiResourceURIMetaKey, got, uiResourceURI("metrics"))
	}
}
