package sse

import (
	"strconv"
	"sync/atomic"
)

const (
	// RetryAfterFloor is the shortest Retry-After a capped stream sends, so a
	// client honouring it exactly cannot come straight back.
	RetryAfterFloor = 5

	// RetryAfterWindow is how many successive seconds above the floor the
	// value steps through.
	RetryAfterWindow = 10
)

var retryAfterTurn atomic.Uint64

// RetryAfter is the delta-seconds a capped stream refuses a client with.
// Successive refusals step through the window, so clients turned away in the
// same instant are told to come back at different times.
func RetryAfter() string {
	turn := retryAfterTurn.Add(1) - 1

	return strconv.Itoa(RetryAfterFloor + int(turn%RetryAfterWindow))
}
