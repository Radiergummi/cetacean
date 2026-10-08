package sse

import (
	"strconv"
	"testing"
)

// Clients turned away together must come back apart, and none sooner than
// the floor: successive refusals step through the whole window above it.
func TestRetryAfterSpreadsAboveTheFloor(t *testing.T) {
	seen := map[int]bool{}
	previous := -1

	for range 2 * RetryAfterWindow {
		seconds, err := strconv.Atoi(RetryAfter())
		if err != nil {
			t.Fatalf("Retry-After is not delta-seconds: %v", err)
		}
		if seconds < RetryAfterFloor || seconds >= RetryAfterFloor+RetryAfterWindow {
			t.Errorf(
				"Retry-After = %d, want [%d, %d)",
				seconds,
				RetryAfterFloor,
				RetryAfterFloor+RetryAfterWindow,
			)
		}
		if seconds == previous {
			t.Errorf("two successive refusals both asked for %d seconds", seconds)
		}

		seen[seconds] = true
		previous = seconds
	}

	if len(seen) != RetryAfterWindow {
		t.Errorf("used %d distinct values, want all %d in the window", len(seen), RetryAfterWindow)
	}
}

// assertRetryAfter fails unless value is one RetryAfter could have produced.
func assertRetryAfter(t *testing.T, value string) {
	t.Helper()

	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < RetryAfterFloor || seconds >= RetryAfterFloor+RetryAfterWindow {
		t.Errorf("Retry-After = %q, want delta-seconds in [%d, %d)",
			value, RetryAfterFloor, RetryAfterFloor+RetryAfterWindow)
	}
}
