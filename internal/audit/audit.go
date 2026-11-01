// Package audit writes one JSON record per audited action, for the operator's
// log pipeline to ship. Storage and retention are deliberately not ours.
package audit

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

// ErrUnavailable is a record that could not be written where failing closed is
// promised.
var ErrUnavailable = errors.New("audit log unavailable")

type Identity struct {
	Subject  string   `json:"subject"`
	Provider string   `json:"provider"`
	Groups   []string `json:"groups,omitempty"`
}

type Target struct {
	Task      string `json:"task"`
	Service   string `json:"service,omitempty"`
	Stack     string `json:"stack,omitempty"`
	Node      string `json:"node,omitempty"`
	Container string `json:"container,omitempty"`
}

type Record struct {
	Time      time.Time `json:"time"`
	Event     string    `json:"event"`
	Session   string    `json:"session,omitempty"`
	Identity  Identity  `json:"identity"`
	Client    string    `json:"client,omitempty"`
	Transport string    `json:"transport,omitempty"`
	Target    *Target   `json:"target,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	EndedBy   string    `json:"endedBy,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
	ExitCode  *int      `json:"exitCode,omitempty"`
	Duration  string    `json:"duration,omitempty"`
	BytesIn   int64     `json:"bytesIn,omitempty"`
	BytesOut  int64     `json:"bytesOut,omitempty"`
}

// line is the record with the routing tag every output carries.
func line(r Record) ([]byte, error) {
	b, err := json.Marshal(struct {
		Log string `json:"log"`
		Record
	}{"audit", r})
	if err != nil {
		return nil, err
	}

	return append(b, '\n'), nil
}

type Logger interface {
	Write(Record) error
}

// Nop is audit switched off.
type Nop struct{}

func (Nop) Write(Record) error { return nil }

// Stream writes to a log stream such as stdout. A log driver can block or drop
// without reporting either, so no write here can fail closed.
type Stream struct {
	mu sync.Mutex
	w  io.Writer
}

func NewStream(w io.Writer) *Stream { return &Stream{w: w} }

func (s *Stream) Write(r Record) error {
	b, err := line(r)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.w.Write(b)

	return nil
}
