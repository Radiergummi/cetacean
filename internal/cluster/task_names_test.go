package cluster_test

import (
	"context"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// tasksOnlyReader admits every task and no service, as a bare `task:*`
// grant does.
func tasksOnlyReader(resource string) bool { return strings.HasPrefix(resource, "task:") }

func hiddenServiceCache() *cache.Cache {
	c := newTestCache()

	svc := swarm.Service{ID: "svc-hidden"}
	svc.Spec.Name = "payroll-db"
	c.SetService(svc)

	task := swarm.Task{ID: "task-hidden", ServiceID: "svc-hidden", Slot: 1}
	task.Spec.ContainerSpec = &swarm.ContainerSpec{Image: "postgres:16"}
	c.SetTask(task)

	return c
}

func TestPresentHistoryNamesOnlyReadableServices(t *testing.T) {
	c := hiddenServiceCache()
	entries := []cache.HistoryEntry{{Type: cache.EventTask, ResourceID: "task-hidden"}}

	if got := cluster.PresentHistory(c, entries, cluster.ReadableTaskNames(c, nil))[0].Name; got != "payroll-db.1" {
		t.Errorf("name with every grant = %q, want payroll-db.1", got)
	}

	if got := cluster.PresentHistory(c, entries, cluster.ReadableTaskNames(c, tasksOnlyReader))[0].Name; got != "task-hidden" {
		t.Errorf("name without the service grant = %q, want the task ID", got)
	}
}

func TestPresentHistoryNamesAPrunedTaskByTheReplicaItRecorded(t *testing.T) {
	c := hiddenServiceCache()
	c.DeleteTask("task-hidden")

	entries := c.History().List(cache.HistoryQuery{Limit: 10})
	if len(entries) < 2 {
		t.Fatalf("history = %+v, want the task's create and remove", entries)
	}

	for _, e := range cluster.PresentHistory(c, entries, cluster.ReadableTaskNames(c, nil)) {
		if e.Type == cache.EventTask && e.Name != "payroll-db.1" {
			t.Errorf("%s of the pruned task named %q, want payroll-db.1", e.Action, e.Name)
		}
	}

	for _, e := range cluster.PresentHistory(c, entries, cluster.ReadableTaskNames(c, tasksOnlyReader)) {
		if e.Type == cache.EventTask && e.Name != "task-hidden" {
			t.Errorf("without the service grant, %s named %q, want the task ID", e.Action, e.Name)
		}
	}

	unrecorded := []cache.HistoryEntry{
		{Type: cache.EventTask, ResourceID: "task-hidden", ServiceID: "svc-hidden"},
	}
	if got := cluster.PresentHistory(c, unrecorded, cluster.ReadableTaskNames(c, nil))[0].Name; got != "task-hidden" {
		t.Errorf("an entry recorded before replicas were kept = %q, want the task ID", got)
	}
}

func TestPresentHistoryNamesATaskRemovedWithItsService(t *testing.T) {
	c := newTestCache()

	replicated := swarm.Service{ID: "svc-web"}
	replicated.Spec.Name = "shop_web"
	c.SetService(replicated)
	c.SetTask(swarm.Task{ID: "task-web", ServiceID: "svc-web", Slot: 2})

	global := swarm.Service{ID: "svc-agent"}
	global.Spec.Name = "shop_agent"
	global.Spec.Mode.Global = &swarm.GlobalService{}
	c.SetService(global)
	c.SetTask(swarm.Task{ID: "task-agent", ServiceID: "svc-agent", NodeID: "node1"})

	// As `docker stack rm` leaves it: tasks and services gone.
	for _, id := range []string{"task-web", "task-agent"} {
		c.DeleteTask(id)
	}
	c.DeleteService("svc-web")
	c.DeleteService("svc-agent")

	want := map[string]string{"task-web": "shop_web.2", "task-agent": "shop_agent.node1"}
	entries := c.History().List(cache.HistoryQuery{
		Types: []cache.EventType{cache.EventTask},
		Limit: 10,
	})

	for _, e := range cluster.PresentHistory(c, entries, cluster.ReadableTaskNames(c, nil)) {
		if e.Name != want[e.ResourceID] {
			t.Errorf("%s of %s named %q, want %q", e.Action, e.ResourceID, e.Name, want[e.ResourceID])
		}
	}

	if got := cluster.ReadableTaskName(c, "task-web", nil); got != "shop_web.2" {
		t.Errorf("a pruned task's own name = %q, want shop_web.2, for its feed's title", got)
	}
	found, _ := cluster.ReadTimeline(c, cache.HistoryQuery{Search: "shop/web.2", Limit: 10}, nil)
	if len(found) != 2 {
		t.Errorf("search for shop/web.2 found %+v, want the task's create and remove", found)
	}
}

func TestReadableTaskNamesNamesEachEntryByWhatItRecorded(t *testing.T) {
	c := hiddenServiceCache()
	c.DeleteTask("task-hidden")

	names := cluster.ReadableTaskNames(c, nil)
	bare := cache.HistoryEntry{Type: cache.EventTask, ResourceID: "task-hidden"}
	recorded := bare
	recorded.ServiceID, recorded.Slot = "svc-hidden", 1

	if got := names(bare); got != "task-hidden" {
		t.Errorf("an entry with no replica = %q, want the ID", got)
	}

	if got := names(recorded); got != "payroll-db.1" {
		t.Errorf("after one with none, an entry with a replica = %q, want payroll-db.1", got)
	}
}

func TestSearchDoesNotRevealAHiddenServiceThroughItsTasks(t *testing.T) {
	c := hiddenServiceCache()

	byName := cluster.Search(context.Background(), c, "payroll", 10, tasksOnlyReader)
	if n := byName.Counts["tasks"]; n != 0 {
		t.Errorf("tasks matched on a hidden service's name: %d", n)
	}

	byImage := cluster.Search(context.Background(), c, "postgres", 10, tasksOnlyReader)
	tasks := byImage.Hits["tasks"]
	if len(tasks) != 1 {
		t.Fatalf("tasks = %v, want the one matching by image", tasks)
	}

	if tasks[0].Name != "task-hidden" {
		t.Errorf("task named %q, want its ID", tasks[0].Name)
	}
}
