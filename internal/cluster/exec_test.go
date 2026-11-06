package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func eligibleTask() swarm.Task {
	return swarm.Task{
		ID: "t1", NodeID: "n1",
		Status: swarm.TaskStatus{
			State:           swarm.TaskStateRunning,
			ContainerStatus: &swarm.ContainerStatus{ContainerID: "c1"},
		},
	}
}

func eligibleNode() swarm.Node {
	n := swarm.Node{ID: "n1"}
	n.Status.State = swarm.NodeStateReady
	n.Spec.Availability = swarm.NodeAvailabilityActive
	n.Description.Platform.OS = "linux"
	return n
}

func notSelf(swarm.Task) ExecIneligibility { return ExecEligible }

func TestExecEligibility(t *testing.T) {
	cases := map[string]struct {
		mutate func(*swarm.Task, *swarm.Node, *bool)
		self   func(swarm.Task) ExecIneligibility
		want   ExecIneligibility
	}{
		"eligible": {func(*swarm.Task, *swarm.Node, *bool) {}, notSelf, ExecEligible},
		"task not running": {
			func(t *swarm.Task, _ *swarm.Node, _ *bool) { t.Status.State = swarm.TaskStateShutdown },
			notSelf,
			ExecTaskNotRunning,
		},
		"no container": {
			func(t *swarm.Task, _ *swarm.Node, _ *bool) { t.Status.ContainerStatus = nil },
			notSelf,
			ExecTaskNotRunning,
		},
		"node unknown": {
			func(_ *swarm.Task, _ *swarm.Node, found *bool) { *found = false },
			notSelf,
			ExecNodeUnavailable,
		},
		"node down": {
			func(_ *swarm.Task, n *swarm.Node, _ *bool) { n.Status.State = swarm.NodeStateDown },
			notSelf,
			ExecNodeUnavailable,
		},
		"node paused": {
			func(_ *swarm.Task, n *swarm.Node, _ *bool) { n.Spec.Availability = swarm.NodeAvailabilityPause },
			notSelf,
			ExecNodeUnavailable,
		},
		"node drained": {
			func(_ *swarm.Task, n *swarm.Node, _ *bool) { n.Spec.Availability = swarm.NodeAvailabilityDrain },
			notSelf,
			ExecNodeUnavailable,
		},
		"windows node": {
			func(_ *swarm.Task, n *swarm.Node, _ *bool) { n.Description.Platform.OS = "windows" },
			notSelf,
			ExecNodeUnsupported,
		},
		"cetacean itself": {
			func(*swarm.Task, *swarm.Node, *bool) {},
			func(swarm.Task) ExecIneligibility { return ExecSelf },
			ExecSelf,
		},
		"cannot tell it from cetacean": {
			func(*swarm.Task, *swarm.Node, *bool) {},
			func(swarm.Task) ExecIneligibility { return ExecSelfUnknown },
			ExecSelfUnknown,
		},
		"a closed node outranks the self check": {
			func(_ *swarm.Task, n *swarm.Node, _ *bool) { n.Status.State = swarm.NodeStateDown },
			func(swarm.Task) ExecIneligibility { return ExecSelf },
			ExecNodeUnavailable,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			task, node, found := eligibleTask(), eligibleNode(), true
			tc.mutate(&task, &node, &found)
			if got := ExecEligibility(task, node, found, tc.self); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
