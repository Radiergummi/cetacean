package projection

import (
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
)

func replicated(name string, replicas uint64) swarm.Service {
	return swarm.Service{
		ID: "svc-" + name,
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: name},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: name + ":1@sha256:abc"},
			},
		},
	}
}

func overlay(id, name string) network.Summary {
	return network.Summary{ID: id, Name: name, Driver: "overlay", Scope: "swarm"}
}
