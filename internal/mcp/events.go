package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// maxEventLimit bounds one read. The ring holds 10,000 entries; handing all of
// them to a model is the failure cetacean://history already demonstrates.
const (
	maxEventLimit     = 500
	defaultEventLimit = 100
)

// eventsResult is what get_events answers with.
type eventsResult struct {
	Entries []cluster.TimelineEntry `json:"entries"`
	Total   int                     `json:"total"`

	// Truncated says the window held more than Limit, so a caller narrowing
	// by time knows it is looking at a page rather than the whole answer.
	Truncated bool `json:"truncated"`

	// TrackingSince is the start of the only window this timeline can answer
	// for: the ring is built at startup and is not persisted, so after a
	// restart it begins there, and once it wraps it begins wherever its oldest
	// surviving entry does. Without it a `since` reaching back twelve hours
	// answers identically whether nothing changed or the record only goes back
	// half an hour — and the second is the common case on a cluster with a
	// service restarting in a loop, whose churn evicts everything else.
	// Compare it against the `since` you asked for.
	TrackingSince string `json:"trackingSince,omitempty"`
}

// toolGetEvents serves the change timeline, filtered.
//
// cetacean://history exists and stays: it is subscribable, and a resource is
// the only thing a client can subscribe to. What it cannot do is answer a
// question — it serves a fixed 100 newest entries, which on a cluster with a
// restarting service is minutes of wall-clock and is all task churn. Every
// filter this tool takes is one cache.HistoryQuery already supports and the
// Atom feeds already use; none of them were reachable from the tool surface.
func (s *Server) toolGetEvents(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	since, err := optionalTime(req, "since")
	if err != nil {
		return "", err
	}

	until, err := optionalTime(req, "until")
	if err != nil {
		return "", err
	}

	limit := req.GetInt("limit", defaultEventLimit)
	if limit <= 0 {
		limit = defaultEventLimit
	}
	if limit > maxEventLimit {
		limit = maxEventLimit
	}

	history := s.cache.History()

	// Every filter but the ACL one is pushed into the walk, so the ring copies
	// what matched rather than everything it holds for the caller to reduce
	// afterwards. On a busy cluster recording mostly task churn, a read for
	// types: ["service"] used to copy ten thousand entries to keep a handful.
	//
	// The limit stays the whole ring, and that is deliberate rather than
	// overlooked: the ACL filter below runs after the read, and `total` has to
	// be how many entries the caller may actually read. Bounding the copy would
	// mean counting entries about resources they cannot see — so an unfiltered
	// read still walks the ring, and pays for it to keep the count honest.
	entries := history.List(cache.HistoryQuery{
		ResourceID: req.GetString("resource", ""),
		Types:      requestedTypes(req),
		Limit:      history.Size(),
		After:      since,
		Before:     until,
	})

	// Task naming runs after the cut, because a name only matters for an entry
	// that is returned and every lookup takes the cache's read lock.
	matched := s.filterHistory(ctx, entries)

	total := len(matched)
	truncated := total > limit
	if truncated {
		matched = matched[:limit]
	}

	matched = nameHistoryTasks(s.cache, matched)

	timeline := make([]cluster.TimelineEntry, 0, len(matched))

	for _, e := range matched {
		timeline = append(timeline, cluster.TimelineEntry{
			At:         cluster.TimelineTime(e.Timestamp),
			Kind:       "change",
			Type:       string(e.Type),
			Name:       e.Name,
			ResourceID: e.ResourceID,
			Message:    e.Action,
		})
	}

	cluster.SortTimeline(timeline)

	result := eventsResult{
		Entries:   timeline,
		Total:     total,
		Truncated: truncated,
	}

	if oldest, ok := history.Oldest(); ok {
		result.TrackingSince = oldest.UTC().Format(time.RFC3339Nano)
	}

	return marshalResult(result)
}

// optionalTime parses an RFC 3339 argument, naming the format when it cannot.
// A model that guessed "yesterday" has to be told what is accepted.
func optionalTime(req mcplib.CallToolRequest, arg string) (time.Time, error) {
	raw := req.GetString(arg, "")
	if raw == "" {
		return time.Time{}, nil
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%s: %q is not an RFC 3339 timestamp (e.g. 2026-09-05T14:02:00Z)",
			arg, raw,
		)
	}

	return parsed, nil
}

// requestedTypes reads the `types` array, or returns nil for "all".
//
// An unrecognised name is passed through rather than rejected: it simply
// matches nothing, which is the same answer as filtering it out afterwards and
// spares the caller a second list of valid types to keep in step with the
// cache's own.
func requestedTypes(req mcplib.CallToolRequest) []cache.EventType {
	raw := req.GetStringSlice("types", nil)
	if len(raw) == 0 {
		return nil
	}

	types := make([]cache.EventType, 0, len(raw))
	for _, t := range raw {
		types = append(types, cache.EventType(t))
	}

	return types
}
