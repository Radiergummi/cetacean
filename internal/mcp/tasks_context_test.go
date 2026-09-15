package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cluster"
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

// Retention is how long a finished result is kept, not how long the work may
// take. A one-minute ceiling must not abandon a service update that is still
// converging.
func TestDetachedTaskBudgetIgnoresRetention(t *testing.T) {
	if detachedTaskBudget != 2*cluster.ConvergenceTimeout {
		t.Errorf(
			"detachedTaskBudget = %v, want %v",
			detachedTaskBudget, 2*cluster.ConvergenceTimeout,
		)
	}
}
