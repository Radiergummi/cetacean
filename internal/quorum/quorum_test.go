package quorum

import (
	"slices"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func manager(reachability swarm.Reachability) swarm.Node {
	return swarm.Node{
		Spec:          swarm.NodeSpec{Role: swarm.NodeRoleManager},
		ManagerStatus: &swarm.ManagerStatus{Reachability: reachability},
	}
}

func worker() swarm.Node {
	return swarm.Node{Spec: swarm.NodeSpec{Role: swarm.NodeRoleWorker}}
}

func TestOf(t *testing.T) {
	up, down := manager(swarm.ReachabilityReachable), manager(swarm.ReachabilityUnreachable)
	unknown := manager(swarm.ReachabilityUnknown)
	// Asked to become a manager, but not yet a Raft member.
	promoting := swarm.Node{Spec: swarm.NodeSpec{Role: swarm.NodeRoleManager}}

	tests := []struct {
		name  string
		nodes []swarm.Node
		want  Quorum
	}{
		{"no managers", []swarm.Node{worker()}, Quorum{}},
		{"one manager", []swarm.Node{up, worker()}, Quorum{1, 1, 1, true, 0}},
		{"three healthy", []swarm.Node{up, up, up}, Quorum{3, 3, 2, true, 1}},
		{"three, one down", []swarm.Node{up, up, down}, Quorum{3, 2, 2, true, 0}},
		{"three, two down", []swarm.Node{up, down, down}, Quorum{3, 1, 2, false, 0}},
		{"four healthy", []swarm.Node{up, up, up, up}, Quorum{4, 4, 3, true, 1}},
		{"five healthy", []swarm.Node{up, up, up, up, up}, Quorum{5, 5, 3, true, 2}},
		{"unknown counts as reachable", []swarm.Node{up, up, unknown}, Quorum{3, 3, 2, true, 1}},
		{
			"a promotion in flight is not a member",
			[]swarm.Node{up, up, up, promoting},
			Quorum{3, 3, 2, true, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Of(slices.Values(tt.nodes)); got != tt.want {
				t.Errorf("Of() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
