package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// A service already in its desired state returns at once — the common case,
// and the one that must not cost a timeout.
func TestWatchReturnsImmediatelyWhenAlreadyConverged(t *testing.T) {
	c := cache.New(nil)
	one := uint64(1)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
		},
	})
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "svc1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.toolWatch(context.Background(), toolRequest(t, "watch", map[string]any{
		"service": "web",
		"timeout": float64(2),
	}))
	if err != nil {
		t.Fatalf("toolWatch: %v", err)
	}

	var got watchResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Outcome != "converged" {
		t.Errorf("outcome = %q, want converged", got.Outcome)
	}
}

// A service that never converges must report why, and must respect the
// caller's timeout rather than the five-minute default.
func TestWatchTimesOutWithTheLastProgressLine(t *testing.T) {
	c := cache.New(nil)
	three := uint64(3)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &three}},
		},
	})

	srv := newResourceTestServer(t, c)

	body, err := srv.toolWatch(context.Background(), toolRequest(t, "watch", map[string]any{
		"service": "web",
		"timeout": float64(1),
	}))
	if err != nil {
		t.Fatalf("toolWatch: %v", err)
	}

	var got watchResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Outcome != "timeout" {
		t.Errorf("outcome = %q, want timeout", got.Outcome)
	}
	if got.Observed == "" {
		t.Error("Observed is empty; a timeout must say how far it got")
	}
}

func TestWatchRejectsAnUnknownService(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	_, err := srv.toolWatch(context.Background(), toolRequest(t, "watch", map[string]any{
		"service": "nosuch",
	}))
	if err == nil {
		t.Fatal("expected an error for an unknown service")
	}
}

// The wait is detached from the request context, so tasks/cancel cannot
// interrupt it and the timeout is the only bound there is. A caller spelling
// "no preference" as 0 must get the documented minute, not the five-minute
// ceiling they cannot escape.
func TestWatchTimeoutFallsBackToTheDefaultNotTheCeiling(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want time.Duration
	}{
		{"unset", map[string]any{}, defaultWatchTimeout},
		{"zero", map[string]any{"timeout": float64(0)}, defaultWatchTimeout},
		{"negative", map[string]any{"timeout": float64(-1)}, defaultWatchTimeout},
		{"explicit", map[string]any{"timeout": float64(30)}, 30 * time.Second},
		{"over the ceiling", map[string]any{"timeout": float64(9000)}, maxWatchTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := watchTimeout(toolRequest(t, "watch", tc.args)); got != tc.want {
				t.Errorf("watchTimeout = %s, want %s", got, tc.want)
			}
		})
	}
}
