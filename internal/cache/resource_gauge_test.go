package cache

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/metrics"
)

func cachedResources(t *testing.T, typ string) float64 {
	t.Helper()

	families, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}

	for _, family := range families {
		if family.GetName() != "cetacean_cache_resources" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "type" && label.GetValue() == typ {
					return metric.GetGauge().GetValue()
				}
			}
		}
	}

	t.Fatalf("no cetacean_cache_resources sample for %q", typ)

	return 0
}

// The gauge follows every create and remove, not only the periodic full sync.
func TestCacheResourcesGaugeFollowsIncrementalChanges(t *testing.T) {
	c := New(nil)
	c.ReplaceAll(FullSyncData{HasServices: true, HasTasks: true})

	c.SetService(swarm.Service{ID: "a"})
	c.SetService(swarm.Service{ID: "b"})
	c.SetTask(swarm.Task{ID: "t"})

	if got := cachedResources(t, "services"); got != 2 {
		t.Errorf("services = %v after two creates, want 2", got)
	}
	if got := cachedResources(t, "tasks"); got != 1 {
		t.Errorf("tasks = %v after one create, want 1", got)
	}

	c.DeleteService("a")

	if got := cachedResources(t, "services"); got != 1 {
		t.Errorf("services = %v after a remove, want 1", got)
	}
}
