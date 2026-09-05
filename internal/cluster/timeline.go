package cluster

import (
	"slices"
	"strings"
)

// TimelineEntry is one thing that happened, whether Cetacean observed it as a
// resource change or a container wrote it to stdout.
//
// The two share a shape so that "this started at 14:02 — what else happened?"
// is a single ordered read rather than a correlation the caller performs
// across two payloads with different field names and different time formats.
// That question is the one incident response actually turns on, and it was
// unanswerable while history and logs were separate shapes.
type TimelineEntry struct {
	// At is RFC 3339, always UTC, at fixed nanosecond width so string
	// comparison is time comparison and a cursor can be a plain string.
	At string `json:"at"`

	// Kind is "change" or "log".
	Kind string `json:"kind"`

	// Type is the resource type for a change ("service", "task", ...), and
	// the stream for a log line ("stdout", "stderr").
	Type string `json:"type,omitempty"`

	// Name is the resource the entry concerns, named the way a reader would
	// say it: a service's name, a task's "<service>.<slot>".
	Name string `json:"name,omitempty"`

	// ResourceID addresses the resource, for a follow-up describe.
	ResourceID string `json:"resourceId,omitempty"`

	// Message is the change's action ("create", "update", "delete") or the
	// log line's text.
	Message string `json:"message,omitempty"`
}

// SortTimeline orders entries newest first, breaking ties on kind and message.
//
// The tie-break exists for determinism rather than meaning: a result may be
// cached by ETag, and two calls that saw identical data must serialise
// identically. Map iteration and merge order do not guarantee that on their
// own.
func SortTimeline(entries []TimelineEntry) {
	slices.SortStableFunc(entries, func(a, b TimelineEntry) int {
		if c := strings.Compare(b.At, a.At); c != 0 {
			return c
		}
		if c := strings.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}

		return strings.Compare(a.Message, b.Message)
	})
}
