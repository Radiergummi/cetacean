package cache

import (
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
)

// Every list the cache builds walks a Go map, whose iteration order is
// randomized per range. Without an explicit order the API's stable sort has a
// shuffled base to work from, so equal sort keys — and any list the handlers
// don't sort at all — come back in a different order on every request.

func stackedService(id, name, stack string) swarm.Service {
	service := swarm.Service{ID: id}
	service.Spec.Name = name
	service.Spec.Labels = map[string]string{"com.docker.stack.namespace": stack}

	return service
}

func TestListsAreStableAcrossCalls(t *testing.T) {
	c := New(nil)

	for i := range 25 {
		c.SetNode(swarm.Node{ID: fmt.Sprintf("node%02d", i)})
		c.SetService(stackedService(
			fmt.Sprintf("svc%02d", i),
			fmt.Sprintf("web_app%02d", i),
			"web",
		))
		c.SetConfig(swarm.Config{ID: fmt.Sprintf("cfg%02d", i)})
		c.SetSecret(swarm.Secret{ID: fmt.Sprintf("sec%02d", i)})
		c.SetNetwork(network.Summary{ID: fmt.Sprintf("net%02d", i)})
		c.SetVolume(volume.Volume{Name: fmt.Sprintf("vol%02d", i)})
		c.SetTask(swarm.Task{ID: fmt.Sprintf("task%02d", i), ServiceID: "svc00", NodeID: "node00"})
	}

	sequences := map[string]func() []string{
		"nodes": func() []string { return ids(c.ListNodes(), func(n swarm.Node) string { return n.ID }) },
		"services": func() []string {
			return ids(c.ListServices(), func(s swarm.Service) string { return s.ID })
		},
		"tasks": func() []string { return ids(c.ListTasks(), func(t swarm.Task) string { return t.ID }) },
		"configs": func() []string {
			return ids(c.ListConfigs(), func(cfg swarm.Config) string { return cfg.ID })
		},
		"secrets": func() []string {
			return ids(c.ListSecrets(), func(s swarm.Secret) string { return s.ID })
		},
		"networks": func() []string {
			return ids(c.ListNetworks(), func(n network.Summary) string { return n.ID })
		},
		"volumes": func() []string {
			return ids(c.ListVolumes(), func(v volume.Volume) string { return v.Name })
		},
		"stacks": func() []string { return ids(c.ListStacks(), func(s Stack) string { return s.Name }) },
		"stackSummaries": func() []string {
			return ids(c.ListStackSummaries(), func(s StackSummary) string { return s.Name })
		},
		"tasksByService": func() []string {
			return ids(c.ListTasksByService("svc00"), func(t swarm.Task) string { return t.ID })
		},
		"tasksByNode": func() []string {
			return ids(c.ListTasksByNode("node00"), func(t swarm.Task) string { return t.ID })
		},
	}

	for name, list := range sequences {
		t.Run(name, func(t *testing.T) {
			first := list()
			if len(first) == 0 {
				t.Fatal("empty list — fixture does not exercise this path")
			}

			for attempt := range 20 {
				got := list()

				for i := range got {
					if got[i] != first[i] {
						t.Fatalf(
							"call %d differs at index %d: %q != %q\nfirst: %v\ngot:   %v",
							attempt+2, i, got[i], first[i], first, got,
						)
					}
				}
			}
		})
	}
}

func TestStackSummariesSortedByName(t *testing.T) {
	c := New(nil)
	for i, name := range []string{"zulu", "alpha", "mike"} {
		c.SetService(stackedService(fmt.Sprintf("svc%d", i), name+"_web", name))
	}

	names := ids(c.ListStackSummaries(), func(s StackSummary) string { return s.Name })
	want := []string{"alpha", "mike", "zulu"}

	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
}

func TestServicesUsingConfigSortedByName(t *testing.T) {
	c := New(nil)
	for i, name := range []string{"zulu", "alpha", "mike"} {
		service := swarm.Service{ID: fmt.Sprintf("svc%d", i)}
		service.Spec.Name = name
		service.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
			Configs: []*swarm.ConfigReference{{ConfigID: "cfg1"}},
		}
		c.SetService(service)
	}

	names := ids(c.ServicesUsingConfig("cfg1"), func(r ServiceRef) string { return r.Name })
	want := []string{"alpha", "mike", "zulu"}

	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
}

func ids[T any](items []T, key func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, key(item))
	}

	return out
}
