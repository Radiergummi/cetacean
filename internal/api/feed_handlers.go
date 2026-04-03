package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// feedEntry is a format-agnostic representation of a single feed entry.
// Both Atom and JSON Feed renderers convert from this type.
type feedEntry struct {
	ID          string
	Title       string
	ContentHTML string
	URL         string
	Updated     time.Time
	Tags        []string
}

// feedData holds the format-agnostic data needed to render a feed.
type feedData struct {
	Title      string
	Entries    []feedEntry
	Updated    time.Time
	BeforeID   uint64
	Limit      int
	LastItemID uint64 // numeric ID of last entry for pagination cursors
}

// feedRenderer writes a feed in a specific format (Atom, JSON Feed, etc.).
type feedRenderer func(w http.ResponseWriter, r *http.Request, data feedData)

// feedListHandler returns a handler that queries history for the given event
// type and renders it using the provided renderer.
func (h *Handlers) feedListHandler(
	title string,
	eventType cache.EventType,
	render feedRenderer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireAnyGrant(w, r) {
			return
		}

		beforeID, limit := parseFeedPagination(r)
		entries := h.cache.History().List(cache.HistoryQuery{
			Type:     eventType,
			BeforeID: beforeID,
			Limit:    limit,
		})
		entries = h.filterHistoryACL(r, entries)

		render(w, r, historyFeedData(r, title, entries, beforeID, limit))
	}
}

// feedDetailHandler returns a handler that queries history for a single
// resource and renders it using the provided renderer.
func (h *Handlers) feedDetailHandler(
	eventType cache.EventType,
	idParam string,
	nameFunc func(id string) string,
	render feedRenderer,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireAnyGrant(w, r) {
			return
		}

		resourceID := r.PathValue(idParam)

		beforeID, limit := parseFeedPagination(r)
		entries := h.cache.History().List(cache.HistoryQuery{
			Type:       eventType,
			ResourceID: resourceID,
			BeforeID:   beforeID,
			Limit:      limit,
		})
		entries = h.filterHistoryACL(r, entries)

		render(w, r, historyFeedData(r, nameFunc(resourceID), entries, beforeID, limit))
	}
}

// handleFeedHistory queries global history and renders it using the given renderer.
func (h *Handlers) handleFeedHistory(
	w http.ResponseWriter,
	r *http.Request,
	render feedRenderer,
) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	beforeID, limit := parseFeedPagination(r)
	entries := h.cache.History().List(cache.HistoryQuery{
		BeforeID: beforeID,
		Limit:    limit,
	})
	entries = h.filterHistoryACL(r, entries)

	render(w, r, historyFeedData(r, "History", entries, beforeID, limit))
}

// handleFeedSearch validates the search query, queries matching history entries,
// and renders them using the given renderer.
func (h *Handlers) handleFeedSearch(
	w http.ResponseWriter,
	r *http.Request,
	render feedRenderer,
) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErrorCode(w, r, "SEA001", "missing required query parameter: q")
		return
	}

	if len(q) > 200 {
		writeErrorCode(w, r, "SEA002", "query too long (max 200 characters)")
		return
	}

	beforeID, limit := parseFeedPagination(r)
	entries := h.cache.History().List(cache.HistoryQuery{
		BeforeID:     beforeID,
		NameContains: q,
		Limit:        limit,
	})
	entries = h.filterHistoryACL(r, entries)

	render(w, r, historyFeedData(
		r, fmt.Sprintf("Cetacean — Search: %s", q), entries, beforeID, limit,
	))
}

// handleFeedRecommendations queries recommendations and renders them using the
// given renderer.
func (h *Handlers) handleFeedRecommendations(
	w http.ResponseWriter,
	r *http.Request,
	render feedRenderer,
) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	results := acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		h.recEngine.Results(),
		recommendationResource,
	)

	lastTick := h.recEngine.LastTick()
	if lastTick.IsZero() {
		lastTick = time.Now()
	}

	entries := make([]feedEntry, 0, len(results))
	for _, rec := range results {
		entries = append(entries, recommendationToFeedEntry(r, rec, lastTick))
	}

	render(w, r, feedData{
		Title:   "Recommendations",
		Entries: entries,
		Updated: lastTick,
	})
}

// feedHistoryHandler wraps handleFeedHistory as an http.HandlerFunc.
func (h *Handlers) feedHistoryHandler(render feedRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.handleFeedHistory(w, r, render)
	}
}

// feedSearchHandler wraps handleFeedSearch as an http.HandlerFunc.
func (h *Handlers) feedSearchHandler(render feedRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.handleFeedSearch(w, r, render)
	}
}

// feedRecommendationsHandler wraps handleFeedRecommendations as an http.HandlerFunc.
func (h *Handlers) feedRecommendationsHandler(render feedRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.handleFeedRecommendations(w, r, render)
	}
}

// actionVerb maps history action strings to past-tense verbs for feed titles.
var actionVerb = map[string]string{
	"update":      "Updated",
	"remove":      "Removed",
	"ref_changed": "Updated",
}

// historyFeedData builds a feedData from history entries, including the
// last entry's numeric ID for pagination cursors.
func historyFeedData(
	r *http.Request,
	title string,
	entries []cache.HistoryEntry,
	beforeID uint64,
	limit int,
) feedData {
	var lastItemID uint64
	if len(entries) > 0 {
		lastItemID = entries[len(entries)-1].ID
	}

	return feedData{
		Title:      title,
		Entries:    historyToFeedEntries(r, entries),
		Updated:    historyUpdated(entries),
		BeforeID:   beforeID,
		Limit:      limit,
		LastItemID: lastItemID,
	}
}

// historyToFeedEntries converts cache history entries to format-agnostic feed entries.
func historyToFeedEntries(r *http.Request, entries []cache.HistoryEntry) []feedEntry {
	result := make([]feedEntry, 0, len(entries))

	for _, e := range entries {
		verb := actionVerb[e.Action]
		if verb == "" && e.Action != "" {
			verb = strings.ToUpper(e.Action[:1]) + e.Action[1:]
		}

		var href string
		if path := resourcePath(e.Type, e.ResourceID); path != "" {
			href = absURL(r, path)
		}

		result = append(result, feedEntry{
			ID:          fmt.Sprintf("urn:cetacean:history:%d", e.ID),
			Title:       fmt.Sprintf("%s %s: %s", verb, e.Type, e.Name),
			ContentHTML: historyEntryHTML(e, href),
			URL:         href,
			Updated:     e.Timestamp,
			Tags:        []string{string(e.Type)},
		})
	}

	return result
}

// recommendationToFeedEntry converts a recommendation to a format-agnostic feed entry.
func recommendationToFeedEntry(
	r *http.Request,
	rec recommendations.Recommendation,
	updated time.Time,
) feedEntry {
	id := fmt.Sprintf(
		"urn:cetacean:recommendation:%s:%s:%s",
		rec.TargetID, rec.Category, rec.Resource,
	)

	var href string
	if rec.TargetID != "" {
		if path := recommendationTargetPath(rec); path != "" {
			href = absURL(r, path)
		}
	}

	content := "<p>" + rec.Message + "</p>"
	if href != "" {
		content += `<p><a href="` + href + `">View in Cetacean</a></p>`
	}

	return feedEntry{
		ID: id,
		Title: fmt.Sprintf(
			"[%s] %s: %s",
			rec.Severity, rec.Category, rec.TargetName,
		),
		ContentHTML: content,
		URL:         href,
		Updated:     updated,
		Tags:        []string{string(rec.Category), string(rec.Severity)},
	}
}

// historyEntryHTML renders a human-readable HTML snippet for a history entry.
func historyEntryHTML(e cache.HistoryEntry, href string) string {
	verb := actionVerb[e.Action]
	if verb == "" {
		verb = e.Action
	}

	var b strings.Builder
	b.WriteString("<p>")
	b.WriteString(verb)
	b.WriteString(" ")
	b.WriteString(string(e.Type))
	b.WriteString(" <strong>")
	b.WriteString(e.Name)
	b.WriteString("</strong>")
	if e.Summary != "" && e.Summary != e.Action+" "+e.Name {
		b.WriteString(": ")
		b.WriteString(e.Summary)
	}
	b.WriteString("</p>")

	if href != "" {
		b.WriteString(`<p><a href="`)
		b.WriteString(href)
		b.WriteString(`">View in Cetacean</a></p>`)
	}

	return b.String()
}

// recommendationTargetPath maps a recommendation to the URL path of its target.
func recommendationTargetPath(rec recommendations.Recommendation) string {
	switch rec.Scope {
	case recommendations.ScopeService:
		return "/services/" + rec.TargetID
	case recommendations.ScopeNode:
		return "/nodes/" + rec.TargetID
	default:
		return "/cluster"
	}
}

// resourcePath maps an event type and resource ID to the URL path for that resource.
func resourcePath(typ cache.EventType, id string) string {
	switch typ {
	case cache.EventNode:
		return "/nodes/" + id
	case cache.EventService:
		return "/services/" + id
	case cache.EventTask:
		return "/tasks/" + id
	case cache.EventConfig:
		return "/configs/" + id
	case cache.EventSecret:
		return "/secrets/" + id
	case cache.EventNetwork:
		return "/networks/" + id
	case cache.EventVolume:
		return "/volumes/" + id
	case cache.EventStack:
		return "/stacks/" + id
	default:
		return ""
	}
}

// filterHistoryACL filters history entries by ACL read permission.
// If no ACL evaluator is configured, returns entries unchanged.
func (h *Handlers) filterHistoryACL(
	r *http.Request,
	entries []cache.HistoryEntry,
) []cache.HistoryEntry {
	if h.acl == nil {
		return entries
	}

	id := auth.IdentityFromContext(r.Context())
	filtered := entries[:0:0]
	for _, e := range entries {
		if h.acl.Can(id, "read", string(e.Type)+":"+e.Name) {
			filtered = append(filtered, e)
		}
	}

	return filtered
}

// feedID builds a tag URI (RFC 4151) for the feed: tag:{host},{year}:{path}.
// The year 2026 is the date the tag namespace was minted and must remain constant.
func feedID(r *http.Request) string {
	return fmt.Sprintf("tag:%s,2026:%s", r.Host, absPath(r.Context(), r.URL.Path))
}

// parseFeedPagination reads ?before= and ?limit= from the query string.
// Default limit is 50, max is 200.
func parseFeedPagination(r *http.Request) (beforeID uint64, limit int) {
	limit = 50

	if v := r.URL.Query().Get("before"); v != "" {
		if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
			beforeID = parsed
		}
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if limit > 200 {
		limit = 200
	}

	return beforeID, limit
}

// emptyFeedEpoch is a stable timestamp for empty feeds so the ETag is
// deterministic (time.Now would produce a different ETag every request).
var emptyFeedEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// historyUpdated returns the timestamp of the most recent history entry,
// or a fixed epoch if the slice is empty.
func historyUpdated(entries []cache.HistoryEntry) time.Time {
	if len(entries) > 0 {
		return entries[0].Timestamp
	}

	return emptyFeedEpoch
}
