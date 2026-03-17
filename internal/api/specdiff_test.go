package api

import (
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

func TestDiffServiceSpecs_NilPrevious(t *testing.T) {
	curr := &swarm.ServiceSpec{}
	if changes := DiffServiceSpecs(nil, curr); changes != nil {
		t.Fatalf("expected nil, got %v", changes)
	}
}

func TestDiffServiceSpecs_Identical(t *testing.T) {
	spec := &swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "svc"},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.24"},
		},
		Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: ptr(uint64(3))}},
	}
	if changes := DiffServiceSpecs(spec, spec); len(changes) != 0 {
		t.Fatalf("expected no changes, got %v", changes)
	}
}

func TestDiffServiceSpecs_ImageChange(t *testing.T) {
	prev := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.24@sha256:abc123"},
		},
	}
	curr := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.25@sha256:def456"},
		},
	}
	changes := DiffServiceSpecs(prev, curr)
	assertChange(t, changes, "Image", "nginx:1.24", "nginx:1.25")
}

func TestDiffServiceSpecs_ReplicaChange(t *testing.T) {
	prev := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "nginx"}},
		Mode:         swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: ptr(uint64(3))}},
	}
	curr := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "nginx"}},
		Mode:         swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: ptr(uint64(5))}},
	}
	changes := DiffServiceSpecs(prev, curr)
	assertChange(t, changes, "Replicas", "3", "5")
}

func TestDiffServiceSpecs_EnvChanges(t *testing.T) {
	prev := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: "app",
				Env:   []string{"LOG_LEVEL=debug", "OLD_VAR=x"},
			},
		},
	}
	curr := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: "app",
				Env:   []string{"LOG_LEVEL=info", "NEW_VAR=y"},
			},
		},
	}
	changes := DiffServiceSpecs(prev, curr)
	assertChange(t, changes, "Env LOG_LEVEL", "debug", "info")
	assertChange(t, changes, "Env removed", "OLD_VAR=x", "")
	assertChange(t, changes, "Env added", "", "NEW_VAR=y")
}

func TestDiffServiceSpecs_MountChanges(t *testing.T) {
	prev := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: "app",
				Mounts: []mount.Mount{
					{Type: mount.TypeVolume, Source: "data", Target: "/data"},
				},
			},
		},
	}
	curr := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: "app",
				Mounts: []mount.Mount{
					{Type: mount.TypeBind, Source: "/host/data", Target: "/data"},
					{Type: mount.TypeTmpfs, Target: "/tmp"},
				},
			},
		},
	}
	changes := DiffServiceSpecs(prev, curr)
	assertChange(t, changes, "Mount /data", "volume:data", "bind:/host/data")
	assertHasField(t, changes, "Mount added")
}

func TestDiffServiceSpecs_ResourceChanges(t *testing.T) {
	prev := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "app"},
			Resources: &swarm.ResourceRequirements{
				Limits: &swarm.Limit{NanoCPUs: 1e9, MemoryBytes: 512 << 20},
			},
		},
	}
	curr := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "app"},
			Resources: &swarm.ResourceRequirements{
				Limits: &swarm.Limit{NanoCPUs: 2e9, MemoryBytes: 1 << 30},
			},
		},
	}
	changes := DiffServiceSpecs(prev, curr)
	assertChange(t, changes, "CPU limit", "1.00 cores", "2.00 cores")
	assertChange(t, changes, "Memory limit", "512 MiB", "1.0 GiB")
}

func TestDiffServiceSpecs_ResourceRemoved(t *testing.T) {
	prev := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "app"},
			Resources: &swarm.ResourceRequirements{
				Limits: &swarm.Limit{NanoCPUs: 1e9, MemoryBytes: 512 << 20},
			},
		},
	}
	curr := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "app"},
		},
	}
	changes := DiffServiceSpecs(prev, curr)
	assertChange(t, changes, "CPU limit", "1.00 cores", "(none)")
	assertChange(t, changes, "Memory limit", "512 MiB", "(none)")
}

func TestDiffServiceSpecs_PortChanges(t *testing.T) {
	prev := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "app"}},
		EndpointSpec: &swarm.EndpointSpec{
			Ports: []swarm.PortConfig{{PublishedPort: 80, TargetPort: 8080, Protocol: "tcp"}},
		},
	}
	curr := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "app"}},
		EndpointSpec: &swarm.EndpointSpec{
			Ports: []swarm.PortConfig{{PublishedPort: 443, TargetPort: 8443, Protocol: "tcp"}},
		},
	}
	changes := DiffServiceSpecs(prev, curr)
	assertChange(t, changes, "Port removed", "80→8080/tcp", "")
	assertChange(t, changes, "Port added", "", "443→8443/tcp")
}

func TestDiffServiceSpecs_PlacementConstraints(t *testing.T) {
	prev := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "app"},
			Placement:     &swarm.Placement{Constraints: []string{"node.role==manager"}},
		},
	}
	curr := &swarm.ServiceSpec{
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: "app"},
			Placement:     &swarm.Placement{Constraints: []string{"node.role==worker"}},
		},
	}
	changes := DiffServiceSpecs(prev, curr)
	assertChange(t, changes, "Placement constraint removed", "node.role==manager", "")
	assertChange(t, changes, "Placement constraint added", "", "node.role==worker")
}

func assertChange(t *testing.T, changes []SpecChange, field, old, new string) {
	t.Helper()
	for _, c := range changes {
		if c.Field == field && c.Old == old && c.New == new {
			return
		}
	}
	t.Errorf("expected change {Field:%q Old:%q New:%q} not found in %v", field, old, new, changes)
}

func assertHasField(t *testing.T, changes []SpecChange, field string) {
	t.Helper()
	for _, c := range changes {
		if c.Field == field {
			return
		}
	}
	t.Errorf("expected change with field %q not found in %v", field, changes)
}

func ptr[T any](v T) *T { return &v }
