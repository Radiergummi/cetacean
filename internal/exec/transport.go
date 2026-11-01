// Package exec opens shells in containers and keeps the sessions they run in.
// Authorization and audit happen in front of a Transport; a transport only
// moves bytes.
package exec

import (
	"context"
	"errors"
)

type Outcome string

const (
	OutcomeExited     Outcome = "exited"
	OutcomeTerminated Outcome = "terminated"
	OutcomeDetached   Outcome = "detached"
)

// Exit is how a shell ended. Code is -1 when it never reported one.
type Exit struct {
	Code    int     `json:"code"`
	Outcome Outcome `json:"outcome"`
}

type Request struct {
	ContainerID string
	NodeID      string
	Cols, Rows  uint
}

type Stream interface {
	// Read returns terminal output, and an error once the shell has ended.
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(ctx context.Context, cols, rows uint) error
	// Close ends the shell if it is still running and reports how it ended.
	Close(ctx context.Context) Exit
}

type Transport interface {
	Name() string
	Reaches(nodeID string) bool
	Open(ctx context.Context, req Request) (Stream, error)
}

var ErrNoShell = errors.New("no shell in this container")

// Select picks the first transport that reaches the node; order is preference.
func Select(transports []Transport, nodeID string) (Transport, bool) {
	for _, t := range transports {
		if t.Reaches(nodeID) {
			return t, true
		}
	}

	return nil, false
}
