package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

func newResourceTestServer(t *testing.T, c *cache.Cache, opts ...func(*Options)) *Server {
	t.Helper()
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	o := Options{
		Config:         cfg,
		GlobalOpsLevel: config.OpsReadOnly,
	}
	for _, fn := range opts {
		fn(&o)
	}
	srv, err := New(c, o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

func TestReadServiceResource(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://services/svc1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	// A read of one resource serves the compact digest, not the raw
	// swarm.Service it used to — the same shape the describe tool builds.
	var digest cluster.Digest
	if err := json.Unmarshal([]byte(body), &digest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if digest.Name != "web" {
		t.Errorf("name = %q, want web", digest.Name)
	}
	if digest.Type != "service" {
		t.Errorf("type = %q, want service", digest.Type)
	}
}

func TestReadNodeResource(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "manager-1"},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://nodes/node1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if !strings.Contains(body, "manager-1") {
		t.Errorf("body missing hostname: %s", body)
	}
}

func TestReadSecretRedactsData(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "db-password"},
			Data:        []byte("super-secret"),
		},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://secrets/sec1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if strings.Contains(body, "super-secret") {
		t.Errorf("secret data leaked into response: %s", body)
	}
	if !strings.Contains(body, "db-password") {
		t.Errorf("body missing secret name: %s", body)
	}
}

func TestReadSecretListRedactsData(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "db-password"},
			Data:        []byte("super-secret"),
		},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://secrets")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if strings.Contains(body, "super-secret") {
		t.Errorf("secret data leaked into list response: %s", body)
	}
}

// TestReadTaskNamesItsParents is what TestReadTaskEnrichesServiceName became
// when the task read started serving a digest: the enriched ServiceName and
// NodeHostname fields are gone, and the parents are named as cross-references
// instead — with real names, which is what the assertion was protecting.
func TestReadTaskNamesItsParents(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})
	c.SetTask(swarm.Task{
		ID:        "task1",
		ServiceID: "svc1",
		NodeID:    "node1",
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://tasks/task1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	var digest cluster.Digest
	if err := json.Unmarshal([]byte(body), &digest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !hasRelated(digest.Related, "service", "web") {
		t.Errorf("task digest does not name its parent service: %s", body)
	}
	if !hasRelated(digest.Related, "node", "worker-1") {
		t.Errorf("task digest does not name its node: %s", body)
	}
}

func TestReadNonexistentResource(t *testing.T) {
	c := cache.New(nil)
	srv := newResourceTestServer(t, c)

	if _, err := srv.readResource(context.Background(), "cetacean://services/missing"); err == nil {
		t.Fatal("expected error for nonexistent service")
	}
}

func TestReadUnknownResourceType(t *testing.T) {
	c := cache.New(nil)
	srv := newResourceTestServer(t, c)

	if _, err := srv.readResource(context.Background(), "cetacean://bananas/whatever"); err == nil {
		t.Fatal("expected error for unknown resource type")
	}
}

func TestReadMalformedURI(t *testing.T) {
	c := cache.New(nil)
	srv := newResourceTestServer(t, c)

	if _, err := srv.readResource(context.Background(), "https://example.com"); err == nil {
		t.Fatal("expected error for non-cetacean URI")
	}
}

func TestReadClusterResource(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})
	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://cluster")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if !strings.Contains(body, `"nodeCount":1`) {
		t.Errorf("cluster snapshot missing nodeCount: %s", body)
	}
}

type stubRecEngine struct {
	results []recommendations.Recommendation
}

func (s *stubRecEngine) Results() []recommendations.Recommendation { return s.results }

func TestReadRecommendationsResource(t *testing.T) {
	c := cache.New(nil)
	rec := &stubRecEngine{results: []recommendations.Recommendation{
		{TargetID: "svc1", TargetName: "web", Message: "Test rec"},
	}}
	srv := newResourceTestServer(t, c, func(o *Options) {
		o.Recommendations = rec
	})

	body, err := srv.readResource(context.Background(), "cetacean://recommendations")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if !strings.Contains(body, "Test rec") {
		t.Errorf("recommendations body missing fixture: %s", body)
	}
}

func TestReadRecommendationsWithoutEngineReturnsEmptyArray(t *testing.T) {
	c := cache.New(nil)
	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://recommendations")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if strings.TrimSpace(body) != "[]" {
		t.Errorf("expected empty array, got %q", body)
	}
}
