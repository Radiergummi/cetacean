package cluster_test

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

func TestSearchByName(t *testing.T) {
	c := newTestCache()

	svc := swarm.Service{}
	svc.ID = "svc-abc"
	svc.Spec.Name = "web-frontend"
	c.SetService(svc)

	node := swarm.Node{}
	node.ID = "node-abc"
	node.Description.Hostname = "worker-node-1"
	c.SetNode(node)

	// Search by service name.
	svcResults := cluster.Search(context.Background(), c, "web-frontend", 10)
	services, ok := svcResults.Hits["services"]
	if !ok {
		t.Fatal("expected services in results")
	}
	if len(services) != 1 {
		t.Fatalf("len(services) = %d, want 1", len(services))
	}
	if services[0].Name != "web-frontend" {
		t.Errorf("services[0].Name = %q, want %q", services[0].Name, "web-frontend")
	}
	if services[0].ID != "svc-abc" {
		t.Errorf("services[0].ID = %q, want %q", services[0].ID, "svc-abc")
	}
	if services[0].Type != "services" {
		t.Errorf("services[0].Type = %q, want %q", services[0].Type, "services")
	}

	// Search by node hostname.
	nodeResultsMap := cluster.Search(context.Background(), c, "worker-node-1", 10)
	nodeResults, ok := nodeResultsMap.Hits["nodes"]
	if !ok {
		t.Fatal("expected nodes in results")
	}
	if len(nodeResults) != 1 {
		t.Fatalf("len(nodeResults) = %d, want 1", len(nodeResults))
	}
	if nodeResults[0].Name != "worker-node-1" {
		t.Errorf("nodeResults[0].Name = %q, want %q", nodeResults[0].Name, "worker-node-1")
	}
}

func TestSearchByLabel(t *testing.T) {
	c := newTestCache()

	svc := swarm.Service{}
	svc.ID = "svc-labeled"
	svc.Spec.Name = "nondescript-service"
	svc.Spec.Labels = map[string]string{"team": "platform-eng"}
	c.SetService(svc)

	results := cluster.Search(context.Background(), c, "platform-eng", 10)

	services, ok := results.Hits["services"]
	if !ok {
		t.Fatal("expected services in results")
	}
	if len(services) != 1 {
		t.Fatalf("len(services) = %d, want 1", len(services))
	}
	if services[0].ID != "svc-labeled" {
		t.Errorf("services[0].ID = %q, want %q", services[0].ID, "svc-labeled")
	}
}

func TestSearchIncludesServiceState(t *testing.T) {
	c := newTestCache()
	replicas := uint64(2)

	svc := swarm.Service{}
	svc.ID = "svc-state"
	svc.Spec.Name = "stateful-service"
	svc.Spec.Mode.Replicated = &swarm.ReplicatedService{Replicas: &replicas}
	c.SetService(svc)

	// No tasks running → expect "failed"
	results := cluster.Search(context.Background(), c, "stateful-service", 10)

	services, ok := results.Hits["services"]
	if !ok {
		t.Fatal("expected services in results")
	}
	if len(services) == 0 {
		t.Fatal("no service results")
	}
	if services[0].State != "failed" {
		t.Errorf("services[0].State = %q, want %q", services[0].State, "failed")
	}
}

func TestSearchRedactsSecrets(t *testing.T) {
	c := newTestCache()

	// Note: the cache's onSet hook for secrets already clears Spec.Data on
	// storage. Search additionally calls RedactSecret on each secret retrieved
	// from the cache, ensuring data is never exposed even if cache behavior changes.
	sec := swarm.Secret{}
	sec.ID = "sec-redact"
	sec.Spec.Name = "my-secret-token"
	sec.Spec.Data = []byte("do-not-expose")
	c.SetSecret(sec)

	results := cluster.Search(context.Background(), c, "my-secret-token", 10)

	secrets, ok := results.Hits["secrets"]
	if !ok {
		t.Fatal("expected secrets in results")
	}
	if len(secrets) == 0 {
		t.Fatal("no secret results")
	}
	// SearchResult has no Data field — secrets cannot be leaked via the result type.
	if secrets[0].Name != "my-secret-token" {
		t.Errorf("secrets[0].Name = %q, want %q", secrets[0].Name, "my-secret-token")
	}
	if secrets[0].ID != "sec-redact" {
		t.Errorf("secrets[0].ID = %q, want %q", secrets[0].ID, "sec-redact")
	}
	if secrets[0].Type != "secrets" {
		t.Errorf("secrets[0].Type = %q, want %q", secrets[0].Type, "secrets")
	}
}

func TestSearchLimit(t *testing.T) {
	c := newTestCache()

	for i := range 5 {
		svc := swarm.Service{}
		svc.ID = "svc-lim-" + string(rune('a'+i))
		svc.Spec.Name = "limit-service-" + string(rune('a'+i))
		c.SetService(svc)
	}

	results := cluster.Search(context.Background(), c, "limit-service", 2)

	services, ok := results.Hits["services"]
	if !ok {
		t.Fatal("expected services in results")
	}
	if results.Counts["services"] != 5 {
		t.Errorf("counts[services] = %d, want 5 (pre-cap total)", results.Counts["services"])
	}
	if results.Total != 5 {
		t.Errorf("total = %d, want 5", results.Total)
	}
	if len(services) > 2 {
		t.Errorf("len(services) = %d, want at most 2 (limit enforced)", len(services))
	}
}

// TestSearchResultsMarshalLikeTheRESTResponse — the same search is served over
// HTTP and over MCP, and this package exists so the two cannot describe it
// differently. Without JSON tags the struct marshalled its Go field names, so
// an MCP client saw `Hits` where an HTTP client saw `results`.
func TestSearchResultsMarshalLikeTheRESTResponse(t *testing.T) {
	encoded, err := json.Marshal(cluster.SearchResults{
		Hits: map[string][]cluster.SearchResult{
			"services": {{Type: "services", ID: "a", Name: "web"}},
		},
		Counts: map[string]int{"services": 1},
		Total:  1,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"results", "counts", "total"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing key %q; got %v", key, slices.Sorted(maps.Keys(decoded)))
		}
	}

	for _, key := range []string{"Hits", "Counts", "Total"} {
		if _, ok := decoded[key]; ok {
			t.Errorf("key %q leaks the Go field name", key)
		}
	}
}
