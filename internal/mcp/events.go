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
	if limit <= 0 || limit > maxEventLimit {
		limit = maxEventLimit
	}

	wanted := requestedTypes(req)

	// Over-read, because the type and time filters are applied after the ring
	// returns and would otherwise silently shorten the page.
	entries := s.cache.History().List(cache.HistoryQuery{
		ResourceID: req.GetString("resource", ""),
		Limit:      maxEventLimit * 4,
	})
	entries = s.filterHistory(ctx, entries)
	entries = nameHistoryTasks(s.cache, entries)

	timeline := make([]cluster.TimelineEntry, 0, len(entries))

	for _, e := range entries {
		if len(wanted) > 0 && !wanted[string(e.Type)] {
			continue
		}
		if !since.IsZero() && !e.Timestamp.After(since) {
			continue
		}
		if !until.IsZero() && e.Timestamp.After(until) {
			continue
		}

		timeline = append(timeline, cluster.TimelineEntry{
			At:         e.Timestamp.UTC().Format(time.RFC3339Nano),
			Kind:       "change",
			Type:       string(e.Type),
			Name:       e.Name,
			ResourceID: e.ResourceID,
			Message:    e.Action,
		})
	}

	cluster.SortTimeline(timeline)

	total := len(timeline)
	truncated := total > limit
	if truncated {
		timeline = timeline[:limit]
	}

	return marshalResult(eventsResult{
		Entries:   timeline,
		Total:     total,
		Truncated: truncated,
	})
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

// requestedTypes reads the `types` array into a set, or returns nil for "all".
func requestedTypes(req mcplib.CallToolRequest) map[string]bool {
	raw := req.GetStringSlice("types", nil)
	if len(raw) == 0 {
		return nil
	}

	set := make(map[string]bool, len(raw))
	for _, t := range raw {
		set[t] = true
	}

	return set
}
