package cluster_test

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
)

func TestSearchResultACLResource(t *testing.T) {
	hit := cluster.SearchResult{ID: "abc123", Name: "web.1"}

	for resourceType, want := range map[string]string{
		"tasks":    "task:abc123",
		"services": "service:web.1",
		"nodes":    "node:web.1",
		"volumes":  "volume:web.1",
	} {
		if got := cluster.SearchResultACLResource(resourceType, hit); got != want {
			t.Errorf("SearchResultACLResource(%q) = %q, want %q", resourceType, got, want)
		}
	}
}

func TestNodeACLName(t *testing.T) {
	named := swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-1"}}
	if got := cluster.NodeACLName(named); got != "worker-1" {
		t.Errorf("NodeACLName(with hostname) = %q, want %q", got, "worker-1")
	}

	if got := cluster.NodeACLName(swarm.Node{ID: "n1"}); got != "n1" {
		t.Errorf("NodeACLName(without hostname) = %q, want %q", got, "n1")
	}
}
