package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestHandleListServices_DesiredAndJobCounts(t *testing.T) {
	c := cache.New(nil)

	for _, id := range []string{"n1", "n2", "n3"} {
		node := swarm.Node{ID: id}
		node.Status.State = swarm.NodeStateReady
		node.Spec.Availability = swarm.NodeAvailabilityActive
		c.SetNode(node)
	}

	drained := swarm.Node{ID: "n4"}
	drained.Status.State = swarm.NodeStateReady
	drained.Spec.Availability = swarm.NodeAvailabilityDrain
	c.SetNode(drained)

	service := func(id string, mode swarm.ServiceMode) swarm.Service {
		svc := swarm.Service{ID: id}
		svc.Spec.Name = id
		svc.Spec.Mode = mode

		return svc
	}

	c.SetService(service("replicated", swarm.ServiceMode{
		Replicated: &swarm.ReplicatedService{Replicas: new(uint64(4))},
	}))
	c.SetService(service("global", swarm.ServiceMode{Global: &swarm.GlobalService{}}))
	c.SetService(service("bounded", swarm.ServiceMode{
		ReplicatedJob: &swarm.ReplicatedJob{
			MaxConcurrent:    new(uint64(2)),
			TotalCompletions: new(uint64(5)),
		},
	}))
	c.SetService(service("concurrent", swarm.ServiceMode{
		ReplicatedJob: &swarm.ReplicatedJob{MaxConcurrent: new(uint64(3))},
	}))
	c.SetService(service("global-job", swarm.ServiceMode{GlobalJob: &swarm.GlobalJob{}}))

	job := service("job", swarm.ServiceMode{ReplicatedJob: &swarm.ReplicatedJob{}})
	job.JobStatus = &swarm.JobStatus{JobIteration: swarm.Version{Index: 2}}
	c.SetService(job)

	task := func(id string, iteration uint64, state swarm.TaskState) swarm.Task {
		return swarm.Task{
			ID:           id,
			ServiceID:    "job",
			JobIteration: &swarm.Version{Index: iteration},
			Status:       swarm.TaskStatus{State: state},
		}
	}

	c.SetTask(task("previous", 1, swarm.TaskStateFailed))
	c.SetTask(task("done", 2, swarm.TaskStateComplete))
	c.SetTask(task("rejected", 2, swarm.TaskStateRejected))

	h := newTestHandlers(t, withCache(c))
	w := httptest.NewRecorder()
	h.HandleListServices(w, httptest.NewRequest("GET", "/services", nil))

	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	items := make(map[string]map[string]any, len(resp.Items))
	for _, item := range resp.Items {
		id, _ := item["ID"].(string)
		items[id] = item
	}

	desired := map[string]float64{
		"replicated": 4,
		"global":     3,
		"bounded":    5,
		"concurrent": 3,
		"global-job": 1,
		"job":        1,
	}
	for id, want := range desired {
		if got := items[id]["DesiredTasks"]; got != want {
			t.Errorf("%s: DesiredTasks = %v, want %v", id, got, want)
		}
	}

	if got := items["job"]["CompletedTasks"]; got != float64(1) {
		t.Errorf("job: CompletedTasks = %v, want 1", got)
	}
	if got := items["job"]["FailedTasks"]; got != float64(1) {
		t.Errorf("job: FailedTasks = %v, want 1: an earlier run's failure is not this run's", got)
	}
	if got := items["bounded"]["CompletedTasks"]; got != float64(0) {
		t.Errorf("bounded: CompletedTasks = %v, want 0 on the wire", got)
	}

	for _, id := range []string{"replicated", "global"} {
		for _, field := range []string{"CompletedTasks", "FailedTasks"} {
			if value, ok := items[id][field]; ok {
				t.Errorf("%s: %s = %v, want it absent: only a job has one", id, field, value)
			}
		}
	}
}
