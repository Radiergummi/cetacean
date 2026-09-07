package cluster

import (
	"slices"
	"strings"
	"time"
)

// TimelineEntry is one thing that happened, whether Cetacean observed it as a
// resource change or a container wrote it to stdout. Kind says which.
//
// Only the change side is on this shape so far: get_events answers with it,
// while the log reads still answer with logs.LogLine. What the two already
// share is the *time format* — internal/logs stamps its lines with the same
// fixed-width layout TimelineTime produces — so "this started at 14:02 — what
// else happened?" is a merge on the timestamp across the two payloads rather
// than a reconciliation of two time formats. That question is the one incident
// response actually turns on. Moving the log reads onto this shape would make
// it a single ordered read instead; until that happens, nothing may tell a
// caller the field names already match.
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

// timelineTimeFormat is RFC 3339 at fixed nanosecond width — the same layout
// internal/logs stamps its lines with, so a change and a log line remain
// comparable. time.RFC3339Nano is not usable here because it *trims* trailing
// zeros: ":00Z" and ":00.5Z" then compare on 'Z' against '.', sorting the
// earlier of the two as though it were the newer.
const timelineTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// TimelineTime renders an instant as a TimelineEntry.At value.
func TimelineTime(t time.Time) string {
	return t.UTC().Format(timelineTimeFormat)
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
