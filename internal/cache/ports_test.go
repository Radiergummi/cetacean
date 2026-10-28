package cache

import (
	"reflect"
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func TestListPublishedPorts(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{
		ID: "web",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "shop_web",
				Labels: map[string]string{StackLabel: "shop"},
			},
		},
		Endpoint: swarm.Endpoint{Ports: []swarm.PortConfig{
			{
				Protocol:      swarm.PortConfigProtocolTCP,
				TargetPort:    80,
				PublishedPort: 8080,
				PublishMode:   swarm.PortConfigPublishModeIngress,
			},
			{
				Protocol:      swarm.PortConfigProtocolUDP,
				TargetPort:    53,
				PublishedPort: 53,
				PublishMode:   swarm.PortConfigPublishModeIngress,
			},
		}},
	})
	c.SetService(swarm.Service{
		ID:   "edge",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "edge"}},
		// A host-mode port shows in the endpoint too, but where it is bound
		// only the running tasks can say.
		Endpoint: swarm.Endpoint{Ports: []swarm.PortConfig{
			{
				Protocol:      swarm.PortConfigProtocolTCP,
				TargetPort:    443,
				PublishedPort: 443,
				PublishMode:   swarm.PortConfigPublishModeHost,
			},
		}},
	})
	hostPort := swarm.PortStatus{Ports: []swarm.PortConfig{
		{
			Protocol:      swarm.PortConfigProtocolTCP,
			TargetPort:    443,
			PublishedPort: 443,
			PublishMode:   swarm.PortConfigPublishModeHost,
		},
	}}
	c.SetTask(
		swarm.Task{
			ID:        "t1",
			ServiceID: "edge",
			NodeID:    "n2",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning, PortStatus: hostPort},
		},
	)
	c.SetTask(
		swarm.Task{
			ID:        "t2",
			ServiceID: "edge",
			NodeID:    "n1",
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning, PortStatus: hostPort},
		},
	)
	c.SetTask(
		swarm.Task{
			ID:        "t3",
			ServiceID: "edge",
			NodeID:    "n3",
			Status:    swarm.TaskStatus{State: swarm.TaskStateShutdown, PortStatus: hostPort},
		},
	)
	c.SetService(
		swarm.Service{
			ID:   "worker",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "worker"}},
		},
	)

	want := []PublishedPort{
		{
			Protocol:      "udp",
			TargetPort:    53,
			PublishedPort: 53,
			PublishMode:   "ingress",
			ServiceID:     "web",
			ServiceName:   "shop_web",
			Stack:         "shop",
		},
		{
			Protocol:      "tcp",
			TargetPort:    443,
			PublishedPort: 443,
			PublishMode:   "host",
			ServiceID:     "edge",
			ServiceName:   "edge",
			Nodes:         []string{"n1", "n2"},
		},
		{
			Protocol:      "tcp",
			TargetPort:    80,
			PublishedPort: 8080,
			PublishMode:   "ingress",
			ServiceID:     "web",
			ServiceName:   "shop_web",
			Stack:         "shop",
		},
	}

	if got := c.ListPublishedPorts(); !reflect.DeepEqual(got, want) {
		t.Errorf("ListPublishedPorts() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestListPublishedPorts_HostModeWithoutRunningTasks(t *testing.T) {
	c := New(nil)
	c.SetService(swarm.Service{
		ID:   "edge",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "edge"}},
		Endpoint: swarm.Endpoint{Ports: []swarm.PortConfig{
			{
				Protocol:      swarm.PortConfigProtocolTCP,
				TargetPort:    443,
				PublishedPort: 443,
				PublishMode:   swarm.PortConfigPublishModeHost,
			},
		}},
	})

	if got := c.ListPublishedPorts(); len(got) != 0 {
		t.Errorf("a host-mode port bound nowhere should not be listed, got %+v", got)
	}
}

// Two dynamic host ports of one service can get the same host port on
// different nodes; they stay two rows, ordered by target.
func TestListPublishedPorts_SamePortDifferentTargets(t *testing.T) {
	c := New(nil)
	c.SetService(
		swarm.Service{
			ID:   "edge",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "edge"}},
		},
	)
	for id, target := range map[string]uint32{"n1": 81, "n2": 80} {
		c.SetTask(swarm.Task{ID: "t-" + id, ServiceID: "edge", NodeID: id, Status: swarm.TaskStatus{
			State: swarm.TaskStateRunning,
			PortStatus: swarm.PortStatus{Ports: []swarm.PortConfig{{
				Protocol: swarm.PortConfigProtocolTCP, TargetPort: target, PublishedPort: 30001,
				PublishMode: swarm.PortConfigPublishModeHost,
			}}},
		}})
	}

	got := c.ListPublishedPorts()
	if len(got) != 2 || got[0].TargetPort != 80 || got[1].TargetPort != 81 {
		t.Errorf("ListPublishedPorts() = %+v, want two rows ordered by target", got)
	}
}
