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

func TestNameHistoryTasksNamesOnlyReadableServices(t *testing.T) {
	c := hiddenServiceCache()
	entries := []cache.HistoryEntry{{Type: cache.EventTask, ResourceID: "task-hidden"}}

	if got := cluster.NameHistoryTasks(c, entries, nil)[0].Name; got != "payroll-db.1" {
		t.Errorf("name with every grant = %q, want payroll-db.1", got)
	}

	if got := cluster.NameHistoryTasks(c, entries, tasksOnlyReader)[0].Name; got != "task-hidden" {
		t.Errorf("name without the service grant = %q, want the task ID", got)
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
