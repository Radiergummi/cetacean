package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"cetacean/internal/cache"
)

func TestHandleCluster(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetService(swarm.Service{ID: "s1"})
	h := NewHandlers(c)

	req := httptest.NewRequest("GET", "/api/cluster", nil)
	w := httptest.NewRecorder()
	h.HandleCluster(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var snap cache.ClusterSnapshot
	json.NewDecoder(w.Body).Decode(&snap)
	if snap.NodeCount != 1 || snap.ServiceCount != 1 {
		t.Errorf("unexpected snapshot: %+v", snap)
	}
}

func TestHandleListNodes(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	c.SetNode(swarm.Node{ID: "n2"})
	h := NewHandlers(c)

	req := httptest.NewRequest("GET", "/api/nodes", nil)
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var nodes []swarm.Node
	json.NewDecoder(w.Body).Decode(&nodes)
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestHandleGetNode_Found(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	h := NewHandlers(c)

	req := httptest.NewRequest("GET", "/api/nodes/n1", nil)
	req.SetPathValue("id", "n1")
	w := httptest.NewRecorder()
	h.HandleGetNode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGetNode_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c)

	req := httptest.NewRequest("GET", "/api/nodes/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetNode(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
