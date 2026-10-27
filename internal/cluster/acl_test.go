package cluster_test

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

func TestNodeACLName(t *testing.T) {
	named := swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "worker-1"}}
	if got := cluster.NodeACLName(named); got != "worker-1" {
		t.Errorf("NodeACLName(with hostname) = %q, want %q", got, "worker-1")
	}

	if got := cluster.NodeACLName(swarm.Node{ID: "n1"}); got != "n1" {
		t.Errorf("NodeACLName(without hostname) = %q, want %q", got, "n1")
	}
}

func TestRecommendationACLResource(t *testing.T) {
	for scope, want := range map[recommendations.Scope]string{
		recommendations.ScopeService: "service:web",
		recommendations.ScopeNode:    "node:web",
		recommendations.ScopeNetwork: "network:web",
		recommendations.ScopeCluster: "swarm:cluster",
	} {
		rec := recommendations.Recommendation{Scope: scope, TargetName: "web"}
		if got := cluster.RecommendationACLResource(rec); got != want {
			t.Errorf("%s: got %q, want %q", scope, got, want)
		}
	}
}
