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
			PublishedPort: 53,
			TargetPort:    53,
			Protocol:      "udp",
			PublishMode:   "ingress",
			ServiceID:     "web",
			ServiceName:   "shop_web",
			Stack:         "shop",
		},
		{
			PublishedPort: 443,
			TargetPort:    443,
			Protocol:      "tcp",
			PublishMode:   "host",
			ServiceID:     "edge",
			ServiceName:   "edge",
			Nodes:         []string{"n1", "n2"},
		},
		{
			PublishedPort: 8080,
			TargetPort:    80,
			Protocol:      "tcp",
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
