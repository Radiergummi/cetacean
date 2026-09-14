package cluster

import (
	"slices"
	"strings"
	"time"
)

// TimelineEntry is one thing that happened, whether Cetacean observed it as a
// resource change or a container wrote it to stdout; Kind says which. Only the
// change side is on this shape so far, but both it and logs.LogLine stamp the
// same fixed-width time format, so merging on timestamps needs no work.
type TimelineEntry struct {
	// At is RFC 3339, always UTC, at fixed nanosecond width so string
	// comparison is time comparison and a cursor can be a plain string.
	// Format it with TimelineTime; time.RFC3339Nano cannot be used, for the
	// reason given there.
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

// timelineTimeFormat is RFC 3339 at fixed nanosecond width, the layout
// internal/logs stamps its lines with, so a change and a log line stay
// comparable. RFC3339Nano *trims* trailing zeros, so ":00Z" and ":00.5Z"
// compare on 'Z' against '.' and sort the earlier as the newer.
const timelineTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// TimelineTime renders an instant as a TimelineEntry.At value.
func TimelineTime(t time.Time) string {
	return t.UTC().Format(timelineTimeFormat)
}

// SortTimeline orders entries newest first, breaking ties on kind and message.
// The tie-break is for determinism rather than meaning: a result may be cached
// by ETag, and map iteration and merge order alone do not guarantee two calls
// over identical data serialise identically.
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
