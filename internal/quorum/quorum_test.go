package quorum

import (
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
	reachable, unreachable := manager(
		swarm.ReachabilityReachable,
	), manager(
		swarm.ReachabilityUnreachable,
	)

	tests := []struct {
		name  string
		nodes []swarm.Node
		want  Quorum
	}{
		{"no managers", []swarm.Node{worker()}, Quorum{}},
		{
			"one manager",
			[]swarm.Node{reachable, worker()},
			Quorum{Managers: 1, Reachable: 1, Size: 1, CanLose: 0},
		},
		{
			"three healthy",
			[]swarm.Node{reachable, reachable, reachable},
			Quorum{Managers: 3, Reachable: 3, Size: 2, CanLose: 1},
		},
		{
			"three, one down",
			[]swarm.Node{reachable, reachable, unreachable},
			Quorum{Managers: 3, Reachable: 2, Size: 2, CanLose: 0},
		},
		{
			"three, two down",
			[]swarm.Node{reachable, unreachable, unreachable},
			Quorum{Managers: 3, Reachable: 1, Size: 2, CanLose: 0},
		},
		{
			"four healthy",
			[]swarm.Node{reachable, reachable, reachable, reachable},
			Quorum{Managers: 4, Reachable: 4, Size: 3, CanLose: 1},
		},
		{
			"five healthy",
			[]swarm.Node{reachable, reachable, reachable, reachable, reachable},
			Quorum{Managers: 5, Reachable: 5, Size: 3, CanLose: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Of(tt.nodes); got != tt.want {
				t.Errorf("Of() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestHeld(t *testing.T) {
	if !(Quorum{Managers: 3, Reachable: 2, Size: 2}).Held() {
		t.Error("2 of 3 reachable should hold quorum")
	}

	if (Quorum{Managers: 3, Reachable: 1, Size: 2}).Held() {
		t.Error("1 of 3 reachable should not hold quorum")
	}
}
