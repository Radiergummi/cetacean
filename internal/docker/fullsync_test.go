package docker

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A worker answers its node-local networks and volumes and refuses every
// swarm list, which must not read as a healthy, empty cluster.
func TestFullSyncError(t *testing.T) {
	cases := []struct {
		name    string
		failed  []string
		wantErr bool
	}{
		{"nothing failed", nil, false},
		{"one swarm list failed", []string{"tasks"}, false},
		{"a worker node", []string{"nodes", "services", "tasks", "configs", "secrets"}, true},
		{
			"everything failed",
			[]string{"nodes", "services", "tasks", "configs", "secrets", "networks", "volumes"},
			true,
		},
	}
	for _, tc := range cases {
		failed := make(map[string]bool)
		for _, name := range tc.failed {
			failed[name] = true
		}

		if err := fullSyncError(failed); (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

// A sweep that ran out of time must say so, not only that the lists failed.
func TestFullSyncReportsItsDeadline(t *testing.T) {
	c, err := NewClient("tcp://127.0.0.1:1", TLS{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	if _, err := c.FullSync(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to wrap context.DeadlineExceeded", err)
	}
}
