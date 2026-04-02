package api

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"time"

	atomxml "github.com/radiergummi/cetacean/internal/api/atom"
	"github.com/radiergummi/cetacean/internal/cache"
)

// writeCachedAtom renders an Atom feed with ETag-based conditional caching.
// Returns 304 Not Modified if the client's If-None-Match header matches.
func writeCachedAtom(w http.ResponseWriter, r *http.Request, feed atomxml.Feed) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))

	var buf bytes.Buffer
	if err := atomxml.Render(&buf, feed); err != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeErrorCode(w, r, "API009", "failed to serialize Atom feed")
		return
	}

	body := buf.Bytes()
	etag := computeETag(body)

	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/atom+xml;charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Vary", "Authorization, Cookie")

	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(body) //nolint:errcheck
}

// feedID builds a tag URI for the feed: tag:{host},{year}:{path}.
// Prefers X-Forwarded-Host over r.Host.
func feedID(r *http.Request) string {
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}

	return fmt.Sprintf("tag:%s,2026:%s", host, r.URL.Path)
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

// historyToEntries converts cache history entries to Atom feed entries.
func historyToEntries(r *http.Request, entries []cache.HistoryEntry) []atomxml.Entry {
	result := make([]atomxml.Entry, 0, len(entries))

	for _, e := range entries {
		content := e.Summary
		if content == "" {
			content = e.Action + " " + e.Name
		}

		title := content
		if len(title) > 80 {
			title = title[:80]
		}

		var links []atomxml.Link
		if path := resourcePath(e.Type, e.ResourceID); path != "" {
			links = []atomxml.Link{{
				Rel:  "alternate",
				Href: absPath(r.Context(), path),
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

// paginationLinks builds self, alternate, and (optionally) next links for the feed.
// A next link is included when len(entries) == limit, indicating more entries may exist.
func paginationLinks(r *http.Request, entries []cache.HistoryEntry, beforeID uint64, limit int) []atomxml.Link {
	selfHref := absPath(r.Context(), r.URL.Path)
	if r.URL.RawQuery != "" {
		selfHref += "?" + r.URL.RawQuery
	}

	links := []atomxml.Link{
		{Rel: "self", Href: selfHref, Type: "application/atom+xml"},
		{Rel: "alternate", Href: selfHref, Type: "text/html"},
	}

	if len(entries) == limit && len(entries) > 0 {
		lastID := entries[len(entries)-1].ID
		basePath := absPath(r.Context(), r.URL.Path)
		links = append(links, atomxml.Link{
			Rel:  "next",
			Href: fmt.Sprintf("%s?before=%d&limit=%d", basePath, lastID, limit),
			Type: "application/atom+xml",
		})
	}

	return links
}
