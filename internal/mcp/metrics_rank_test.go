package mcp

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/prom"
)

// labelledSeries answers with one series per named member, so a ranking test
// can assert what each series was named rather than only how many came back.
func labelledSeries(label string, names ...string) []prom.Series {
	series := make([]prom.Series, 0, len(names))

	for i, name := range names {
		series = append(series, prom.Series{
			Labels: map[string]string{label: name},
			Points: []prom.Point{{Timestamp: 1, Value: float64(i + 1)}},
		})
	}

	return series
}

// Intent 30 — "what's using the most CPU?" — is a ranking, and a ranking is
// one series per member. The result shape does not change, so the widget
// charts it unchanged.
func TestGetMetricsRanksServicesClusterWide(t *testing.T) {
	querier := &fakeQuerier{
		series: labelledSeries(swarmServiceLabel, "monitoring_prometheus", "secret-app"),
	}
	srv := metricsServer(t, querier)

	result, err := callGetMetrics(t, srv, map[string]any{
		"target": "cluster",
		"top":    float64(5),
	})
	if err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	if len(result.Series) != 2 {
		t.Fatalf("series = %d, want one per ranked service", len(result.Series))
	}
	if result.Series[0].Name != "monitoring_prometheus" {
		t.Errorf("series[0] = %q, want the service's own name", result.Series[0].Name)
	}

	if len(querier.queries) == 0 || !strings.Contains(querier.queries[0], "topk(5,") {
		t.Errorf("query does not rank: %v", querier.queries)
	}
	if !strings.Contains(querier.queries[0], "by ("+swarmServiceLabel+")") {
		t.Errorf("query does not group by service: %s", querier.queries[0])
	}
}

// The other half of the cluster scope: which host is hot, which node-exporter
// answers on its own — no cAdvisor required.
func TestGetMetricsRanksNodesWhenAsked(t *testing.T) {
	querier := &fakeQuerier{series: labelledSeries("instance", "10.0.0.7:9100")}
	srv := metricsServer(t, querier)

	result, err := callGetMetrics(t, srv, map[string]any{
		"target": "cluster",
		"by":     "node",
		"top":    float64(3),
	})
	if err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	// The instance label is host:port and means nothing to a reader; the
	// cluster's own name for that node does.
	if len(result.Series) != 1 || result.Series[0].Name != "worker-1" {
		t.Errorf("series = %+v, want one named worker-1", result.Series)
	}
}

// An instance no node in the cache claims is still reported — under its raw
// label, since dropping it would silently shorten a ranking.
func TestGetMetricsKeepsAnUnmatchedInstance(t *testing.T) {
	querier := &fakeQuerier{series: labelledSeries("instance", "10.9.9.9:9100")}
	srv := metricsServer(t, querier)

	result, err := callGetMetrics(t, srv, map[string]any{
		"target": "cluster",
		"by":     "node",
	})
	if err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	if len(result.Series) != 1 || result.Series[0].Name != "10.9.9.9:9100" {
		t.Errorf("series = %+v, want the raw instance kept", result.Series)
	}
}

// Intent 33 — "why is node N hot?" — ranks the services on that node. The
// scope is the node's own instance, which is standard Prometheus rather than
// a relabel only Cetacean's shipped config defines.
func TestGetMetricsRanksServicesOnOneNode(t *testing.T) {
	querier := &fakeQuerier{series: labelledSeries(swarmServiceLabel, "monitoring_prometheus")}
	srv := metricsServer(t, querier)

	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "node",
		"id":     "node1",
		"top":    float64(5),
	}); err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	query := querier.queries[0]
	if !strings.Contains(query, `instance=~"10\\.0\\.0\\.7:.*"`) {
		t.Errorf("query is not scoped to the node: %s", query)
	}
	if !strings.Contains(query, "by ("+swarmServiceLabel+")") {
		t.Errorf("query does not group by service: %s", query)
	}
}

// Ranking cluster-wide and then filtering would hand a restricted caller an
// empty answer whenever the true top N are all invisible to them. The
// selector is built from what they may read, so topk ranks only across that.
func TestGetMetricsRanksOnlyWithinTheCallersGrants(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:monitoring_*"))

	querier := &fakeQuerier{series: labelledSeries(swarmServiceLabel, "monitoring_prometheus")}
	srv := metricsServer(t, querier, func(o *Options) { o.ACL = evaluator })

	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "cluster",
		"top":    float64(5),
	}); err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	query := querier.queries[0]
	if !strings.Contains(query, "monitoring_prometheus") {
		t.Errorf("query does not restrict to readable services: %s", query)
	}
	if strings.Contains(query, "secret-app") {
		t.Errorf("query names a service the caller may not read: %s", query)
	}
}

// A caller who may read nothing gets no ranking rather than an unrestricted
// one — the failure that matters, since an empty allow-list read as "no
// filter" would rank the whole cluster for them.
func TestGetMetricsRefusesToRankWithNoReadableMembers(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:nothing-matches-this"))

	querier := &fakeQuerier{}
	srv := metricsServer(t, querier, func(o *Options) { o.ACL = evaluator })

	_, err := callGetMetrics(t, srv, map[string]any{"target": "cluster", "top": float64(5)})
	if err == nil {
		t.Fatal("expected a refusal when the caller may read no services")
	}
	if len(querier.queries) != 0 {
		t.Errorf("queried Prometheus anyway: %v", querier.queries)
	}
}

// The cluster scope is about many members, so naming one is a contradiction
// rather than a narrowing.
func TestGetMetricsRejectsAnIDOnTheClusterTarget(t *testing.T) {
	srv := metricsServer(t, &fakeQuerier{series: onePoint()})

	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "cluster",
		"id":     "svc1",
	}); err == nil {
		t.Fatal("expected an error: the cluster target takes no id")
	}
}

// A service's contributors are its tasks, which this tool does not chart.
func TestGetMetricsRejectsTopOnAService(t *testing.T) {
	srv := metricsServer(t, &fakeQuerier{series: onePoint()})

	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "service",
		"id":     "svc1",
		"top":    float64(5),
	}); err == nil {
		t.Fatal("expected an error: a service cannot be broken down further")
	}
}

// Ranking is capped: a legend of forty series is not a chart anyone reads,
// and it is a lot of points for a model to hold.
func TestGetMetricsCapsTop(t *testing.T) {
	querier := &fakeQuerier{series: labelledSeries(swarmServiceLabel, "monitoring_prometheus")}
	srv := metricsServer(t, querier)

	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "cluster",
		"top":    float64(500),
	}); err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	if !strings.Contains(querier.queries[0], "topk(10,") {
		t.Errorf("top was not capped: %s", querier.queries[0])
	}
}

// The single-target reads must be untouched by any of this.
func TestGetMetricsSingleTargetIsUnchanged(t *testing.T) {
	querier := &fakeQuerier{series: onePoint()}
	srv := metricsServer(t, querier)

	result, err := callGetMetrics(t, srv, map[string]any{"target": "service", "id": "svc1"})
	if err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	if result.Name != "monitoring_prometheus" {
		t.Errorf("name = %q", result.Name)
	}
	if strings.Contains(querier.queries[0], "topk") {
		t.Errorf("a single-target read must not rank: %s", querier.queries[0])
	}
}

// Ranking within one node ranks *services*, so it needs the caller's service
// grants as much as the cluster-wide ranking does. Scoping only to the node's
// instance would name — and chart the load of — every service running on a
// host the caller happens to be able to read.
func TestGetMetricsRanksServicesOnANodeOnlyWithinTheCallersGrants(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("node:*", "service:monitoring_*"))

	querier := &fakeQuerier{series: labelledSeries(swarmServiceLabel, "monitoring_prometheus")}
	srv := metricsServer(t, querier, func(o *Options) { o.ACL = evaluator })

	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "node",
		"id":     "node1",
		"top":    float64(5),
	}); err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	query := querier.queries[0]
	if !strings.Contains(query, `instance=~"10\\.0\\.0\\.7:.*"`) {
		t.Errorf("query is not scoped to the node: %s", query)
	}
	if !strings.Contains(query, "monitoring_prometheus") {
		t.Errorf("query does not restrict to readable services: %s", query)
	}
	if strings.Contains(query, "secret-app") {
		t.Errorf("query names a service the caller may not read: %s", query)
	}
}

// A name's regex metacharacters have to be quoted before it is joined into an
// alternation: `=~` compiles what it is given, so an unquoted "monitoring.api"
// would also match "monitoringxapi" — a service the caller has no grant for.
func TestGetMetricsQuotesRegexMetacharactersInAScope(t *testing.T) {
	c := seedMetricsCache(t)
	c.SetService(swarm.Service{
		ID:   "svc3",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "monitoring.api"}},
	})

	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:monitoring.api"))

	querier := &fakeQuerier{series: labelledSeries(swarmServiceLabel, "monitoring.api")}
	srv := newToolTestServer(
		t, c, &fakeWriteClient{}, config.OpsReadOnly,
		func(o *Options) { o.Prometheus = querier },
		func(o *Options) { o.ACL = evaluator },
	)

	if _, err := callGetMetrics(t, srv, map[string]any{
		"target": "cluster",
		"top":    float64(5),
	}); err != nil {
		t.Fatalf("get_metrics: %v", err)
	}

	query := querier.queries[0]
	if !strings.Contains(query, `monitoring\\.api`) {
		t.Errorf("the dot is not regex-quoted, so the matcher reaches past the grant: %s", query)
	}
}

// An evaluator exists on every deployment that turns authentication on, policy
// or not — and with no policy it grants everything. Keying the selector off the
// evaluator's presence therefore joined every service name in the cluster into
// a PromQL alternation that excluded nothing, on every ranked call.
func TestRankScopeIsEmptyWhenGrantsHideNothing(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"web", "api", "worker"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}
	c.SetNode(swarm.Node{
		ID:          "n1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	// An evaluator with no policy: authenticated callers may read everything.
	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = acl.NewEvaluator() })

	for _, by := range []string{rankByService, "node"} {
		got, err := srv.rankScope(ctxWithIdentity(), by)
		if err != nil {
			t.Fatalf("rankScope(%q): %v", by, err)
		}
		if got != "" {
			t.Errorf("rankScope(%q) = %q, want empty: nothing is hidden from this caller", by, got)
		}
	}
}

// And when grants genuinely narrow the cluster, the selector still carries them
// into the query — ranking everything and filtering the result afterwards would
// hand a restricted caller an empty answer.
func TestRankScopeRestrictsWhenGrantsHideSomething(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"public-api", "secret-svc"} {
		c.SetService(swarm.Service{
			ID:   name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	got, err := srv.rankScope(ctxWithIdentity(), rankByService)
	if err != nil {
		t.Fatalf("rankScope: %v", err)
	}
	if !strings.Contains(got, "public-api") {
		t.Errorf("rankScope = %q, want the readable service named", got)
	}
	if strings.Contains(got, "secret-svc") {
		t.Errorf("rankScope = %q, must not name a service the caller cannot read", got)
	}
}
