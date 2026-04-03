package api

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/radiergummi/cetacean/internal/acl"
	atomxml "github.com/radiergummi/cetacean/internal/api/atom"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// atomListHandler returns a handler that serves history entries of the given
// type as an Atom feed. Used for list endpoints like /nodes, /services, etc.
func (h *Handlers) atomListHandler(title string, eventType cache.EventType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireAnyGrant(w, r) {
			return
		}

		beforeID, limit := parseAtomPagination(r)
		entries := h.cache.History().List(cache.HistoryQuery{
			Type:     eventType,
			BeforeID: beforeID,
			Limit:    limit,
		})
		entries = h.filterHistoryACL(r, entries)

		writeCachedAtom(w, r, atomxml.Feed{
			Title:   title,
			Author:  &atomxml.Author{Name: "Cetacean"},
			ID:      feedID(r),
			Updated: historyUpdated(entries),
			Links:   paginationLinks(r, entries, beforeID, limit),
			Entries: historyToEntries(r, entries),
		})
	}
}

// atomDetailHandler returns a handler that serves history entries for a single
// resource as an Atom feed. Used for detail endpoints like /nodes/{id}.
func (h *Handlers) atomDetailHandler(
	eventType cache.EventType,
	idParam string,
	nameFunc func(id string) string,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.requireAnyGrant(w, r) {
			return
		}

		resourceID := r.PathValue(idParam)

		beforeID, limit := parseAtomPagination(r)
		entries := h.cache.History().List(cache.HistoryQuery{
			Type:       eventType,
			ResourceID: resourceID,
			BeforeID:   beforeID,
			Limit:      limit,
		})
		entries = h.filterHistoryACL(r, entries)

		title := nameFunc(resourceID)

		writeCachedAtom(w, r, atomxml.Feed{
			Title:   title,
			Author:  &atomxml.Author{Name: "Cetacean"},
			ID:      feedID(r),
			Updated: historyUpdated(entries),
			Links:   paginationLinks(r, entries, beforeID, limit),
			Entries: historyToEntries(r, entries),
		})
	}
}

// HandleAtomSearch serves history entries matching a name search as an Atom feed.
func (h *Handlers) HandleAtomSearch(w http.ResponseWriter, r *http.Request) {
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

	beforeID, limit := parseAtomPagination(r)

	entries := h.cache.History().List(cache.HistoryQuery{
		BeforeID:     beforeID,
		NameContains: q,
		Limit:        limit,
	})
	entries = h.filterHistoryACL(r, entries)

	feed := atomxml.Feed{
		Title:   fmt.Sprintf("Cetacean — Search: %s", q),
		Author:  &atomxml.Author{Name: "Cetacean"},
		ID:      feedID(r),
		Updated: historyUpdated(entries),
		Links:   paginationLinks(r, entries, beforeID, limit),
		Entries: historyToEntries(r, entries),
	}
	writeCachedAtom(w, r, feed)
}

// HandleAtomHistory serves the global history feed as Atom (no type filter).
func (h *Handlers) HandleAtomHistory(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	beforeID, limit := parseAtomPagination(r)
	entries := h.cache.History().List(cache.HistoryQuery{
		BeforeID: beforeID,
		Limit:    limit,
	})
	entries = h.filterHistoryACL(r, entries)

	writeCachedAtom(w, r, atomxml.Feed{
		Title:   "History",
		Author:  &atomxml.Author{Name: "Cetacean"},
		ID:      feedID(r),
		Updated: historyUpdated(entries),
		Links:   paginationLinks(r, entries, beforeID, limit),
		Entries: historyToEntries(r, entries),
	})
}

// HandleAtomRecommendations serves recommendation engine results as an Atom feed.
func (h *Handlers) HandleAtomRecommendations(w http.ResponseWriter, r *http.Request) {
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

	atomEntries := make([]atomxml.Entry, 0, len(results))
	for _, rec := range results {
		atomEntries = append(atomEntries, recommendationToEntry(r, rec, lastTick))
	}

	selfHref := absURL(r, r.URL.Path+".atom")
	alternateHref := absURL(r, "/recommendations")
	writeCachedAtom(w, r, atomxml.Feed{
		Title:   "Recommendations",
		Author:  &atomxml.Author{Name: "Cetacean"},
		ID:      feedID(r),
		Updated: lastTick,
		Links: []atomxml.Link{
			{Rel: "self", Href: selfHref, Type: "application/atom+xml"},
			{Rel: "alternate", Href: alternateHref, Type: "text/html"},
		},
		Entries: atomEntries,
	})
}

// recommendationToEntry converts a single recommendation to an Atom entry.
func recommendationToEntry(
	r *http.Request,
	rec recommendations.Recommendation,
	updated time.Time,
) atomxml.Entry {
	id := fmt.Sprintf(
		"urn:cetacean:recommendation:%s:%s:%s",
		rec.TargetID, rec.Category, rec.Resource,
	)

	var links []atomxml.Link
	if rec.TargetID != "" {
		path := recommendationTargetPath(rec)
		if path != "" {
			links = []atomxml.Link{{
				Rel:  "alternate",
				Href: absURL(r, path),
				Type: "text/html",
			}}
		}
	}

	return atomxml.Entry{
		ID: id,
		Title: fmt.Sprintf(
			"[%s] %s: %s",
			rec.Severity, rec.Category, rec.TargetName,
		),
		Updated: updated,
		Content: atomxml.ContentElement{
			Type:  "text",
			Value: rec.Message,
		},
		Links: links,
		Categories: []atomxml.Category{
			{Term: string(rec.Category)},
			{Term: string(rec.Severity)},
		},
	}
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

// writeCachedAtom renders an Atom feed with ETag-based conditional caching.
// Returns 304 Not Modified if the client's If-None-Match header matches.
func writeCachedAtom(w http.ResponseWriter, r *http.Request, feed atomxml.Feed) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))

	var buf bytes.Buffer
	if err := atomxml.Render(&buf, feed); err != nil {
		writeErrorCode(w, r, "API009", "failed to serialize Atom feed")
		return
	}

	body := buf.Bytes()
	etag := computeETag(body)

	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/atom+xml;charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Add("Vary", "Authorization, Cookie")

	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(body) //nolint:errcheck
}

// feedID builds a tag URI (RFC 4151) for the feed: tag:{host},{year}:{path}.
// The year 2026 is the date the tag namespace was minted and must remain constant.
func feedID(r *http.Request) string {
	return fmt.Sprintf("tag:%s,2026:%s", r.Host, absPath(r.Context(), r.URL.Path))
}

// parseAtomPagination reads ?before= and ?limit= from the query string.
// Default limit is 50, max is 200.
func parseAtomPagination(r *http.Request) (beforeID uint64, limit int) {
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

// historyToEntries converts cache history entries to Atom feed entries.
func historyToEntries(r *http.Request, entries []cache.HistoryEntry) []atomxml.Entry {
	result := make([]atomxml.Entry, 0, len(entries))

	for _, e := range entries {
		content := e.Summary
		if content == "" {
			content = e.Action + " " + e.Name
		}

		title := content
		if runes := []rune(title); len(runes) > 80 {
			title = string(runes[:80])
		}

		var links []atomxml.Link
		if path := resourcePath(e.Type, e.ResourceID); path != "" {
			links = []atomxml.Link{{
				Rel:  "alternate",
				Href: absURL(r, path),
				Type: "text/html",
			}}
		}

		result = append(result, atomxml.Entry{
			ID:      fmt.Sprintf("urn:cetacean:history:%d", e.ID),
			Title:   title,
			Updated: e.Timestamp,
			Content: atomxml.ContentElement{
				Type:  "text",
				Value: content,
			},
			Links: links,
			Categories: []atomxml.Category{{
				Term: string(e.Type),
			}},
		})
	}

	return result
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

// paginationLinks builds self, alternate, and (optionally) next/previous links
// for the feed per RFC 5005 (Feed Paging and Archiving).
// A next link is included when len(entries) == limit, indicating more entries may exist.
// A previous link is included on non-first pages, pointing back to the subscription document.
func paginationLinks(
	r *http.Request,
	entries []cache.HistoryEntry,
	beforeID uint64,
	limit int,
) []atomxml.Link {
	atomPath := r.URL.Path + ".atom"

	selfHref := absURL(r, atomPath)
	if r.URL.RawQuery != "" {
		selfHref += "?" + r.URL.RawQuery
	}

	alternateHref := absURL(r, r.URL.Path)
	if aq := r.URL.Query(); len(aq) > 0 {
		aq.Del("before")
		aq.Del("limit")

		if encoded := aq.Encode(); encoded != "" {
			alternateHref += "?" + encoded
		}
	}
	links := []atomxml.Link{
		{Rel: "self", Href: selfHref, Type: "application/atom+xml"},
		{Rel: "alternate", Href: alternateHref, Type: "text/html"},
	}

	// On non-first pages, link back to the subscription document (first page).
	// Per RFC 5005, this should point to the feed document, not the HTML alternate.
	if beforeID > 0 {
		firstPageHref := absURL(r, atomPath)
		if aq := r.URL.Query(); len(aq) > 0 {
			aq.Del("before")
			aq.Del("limit")

			if encoded := aq.Encode(); encoded != "" {
				firstPageHref += "?" + encoded
			}
		}

		links = append(links, atomxml.Link{
			Rel:  "previous",
			Href: firstPageHref,
			Type: "application/atom+xml",
		})
	}

	// When the cursor has been evicted from the ring buffer, the result is
	// empty despite requesting a non-first page. Include a "current" link
	// (RFC 5005 Section 2) so feed readers can recover by restarting from
	// the subscription document.
	if beforeID > 0 && len(entries) == 0 {
		currentHref := absURL(r, atomPath)
		if aq := r.URL.Query(); len(aq) > 0 {
			aq.Del("before")
			aq.Del("limit")

			if encoded := aq.Encode(); encoded != "" {
				currentHref += "?" + encoded
			}
		}

		links = append(links, atomxml.Link{
			Rel:  "current",
			Href: currentHref,
			Type: "application/atom+xml",
		})
	}

	if len(entries) == limit && len(entries) > 0 {
		lastID := entries[len(entries)-1].ID

		// Build next link preserving existing query params.
		q := r.URL.Query()
		q.Set("before", strconv.FormatUint(lastID, 10))
		q.Set("limit", strconv.Itoa(limit))
		nextHref := absURL(r, atomPath) + "?" + q.Encode()

		links = append(links, atomxml.Link{
			Rel:  "next",
			Href: nextHref,
			Type: "application/atom+xml",
		})
	}

	return links
}
