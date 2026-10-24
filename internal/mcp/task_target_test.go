package mcp

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// slotCache holds one live replica of "web" in slot 1, so "web.1" names it.
func slotCache() *cache.Cache {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetTask(swarm.Task{
		ID: "task-live-1", ServiceID: "svc1", Slot: 1,
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	return c
}

func TestGetLogsReadsTheTaskASlotNames(t *testing.T) {
	streamer := &fakeLogStreamer{}
	srv := newLogTestServer(t, slotCache(), streamer)
	td, _ := srv.findTool("get_logs")

	if _, err := td.handler(context.Background(), newCallToolRequest("get_logs", map[string]any{
		"task": "web.1",
	})); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if streamer.calledID != "task-live-1" {
		t.Errorf("streamer id = %q, want the resolved task ID", streamer.calledID)
	}
}

// Docker reads an unresolved argument as an ID prefix, which the ACL never
// checked, so it must not reach the daemon at all.
func TestGetLogsRefusesAnUnresolvedTask(t *testing.T) {
	streamer := &fakeLogStreamer{}
	srv := newLogTestServer(t, slotCache(), streamer)
	td, _ := srv.findTool("get_logs")

	if _, err := td.handler(context.Background(), newCallToolRequest("get_logs", map[string]any{
		"task": "task-live",
	})); err == nil {
		t.Fatal("get_logs read a task prefix")
	}

	if streamer.calledID != "" {
		t.Errorf("streamer called with %q", streamer.calledID)
	}
}

func TestRemoveTaskRemovesTheTaskASlotNames(t *testing.T) {
	var removed string
	wc := &fakeWriteClient{removeTaskFn: func(_ context.Context, id string) error {
		removed = id
		return nil
	}}
	srv := newToolTestServer(t, slotCache(), wc, config.OpsImpactful)
	td, _ := srv.findTool("remove_task")

	if _, err := td.handler(context.Background(), newCallToolRequest("remove_task", map[string]any{
		"id": "web.1",
	})); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if removed != "task-live-1" {
		t.Errorf("removed = %q, want the resolved task ID", removed)
	}
}

func TestRemoveTaskRefusesAnUnresolvedTask(t *testing.T) {
	var removed string
	wc := &fakeWriteClient{removeTaskFn: func(_ context.Context, id string) error {
		removed = id
		return nil
	}}
	srv := newToolTestServer(t, slotCache(), wc, config.OpsImpactful)
	td, _ := srv.findTool("remove_task")

	if _, err := td.handler(context.Background(), newCallToolRequest("remove_task", map[string]any{
		"id": "task-live",
	})); err == nil {
		t.Fatal("remove_task removed a task prefix")
	}

	if removed != "" {
		t.Errorf("removed %q", removed)
	}
}
