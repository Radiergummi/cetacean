package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

// net/http cancels the request context the moment the create-task response is
// written, so the work must outlive it — but not forever: WithoutCancel drops
// the deadline too, and a hung Docker call would then hold its goroutine past
// shutdown's drain.
func TestADetachedTaskContextIsBoundedButNotCancelled(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	ctx, release := detachTaskContext(parent, 50*time.Millisecond)
	defer release()

	if err := ctx.Err(); err != nil {
		t.Fatalf("the detached context starts already cancelled: %v", err)
	}

	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("the detached context carries no deadline, so a hung call is never reclaimed")
	}

	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Error("the detached context never expired")
	}
}

// The budget follows how long the result could still be collected.
func TestDetachedTaskBudgetFollowsRetention(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config config.MCPConfig
		want   time.Duration
	}{
		{"ceiling wins", config.MCPConfig{MaxTaskTTL: time.Minute, TaskTTL: time.Hour}, time.Minute},
		{"default when uncapped", config.MCPConfig{TaskTTL: 30 * time.Minute}, 30 * time.Minute},
		{"neither configured", config.MCPConfig{}, 2 * cluster.ConvergenceTimeout},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{config: tt.config}

			if got := s.detachedTaskBudget(); got != tt.want {
				t.Errorf("detachedTaskBudget() = %v, want %v", got, tt.want)
			}
		})
	}
}
