package api

import (
	"bytes"
	"net/http"
	"strconv"
	"time"

	atomxml "github.com/radiergummi/cetacean/internal/api/atom"
)

// renderAtom converts format-agnostic feedData to an Atom feed and writes it.
func renderAtom(w http.ResponseWriter, r *http.Request, data feedData) {
	writeCachedAtom(w, r, atomxml.Feed{
		Title:   data.Title,
		Author:  &atomxml.Author{Name: "Cetacean"},
		ID:      feedID(r),
		Updated: data.Updated,
		Links:   atomPaginationLinks(r, data),
		Entries: feedEntriesToAtom(r, data.Entries),
	})
}

// feedEntriesToAtom converts format-agnostic feed entries to Atom entries.
func feedEntriesToAtom(r *http.Request, entries []feedEntry) []atomxml.Entry {
	result := make([]atomxml.Entry, 0, len(entries))

	for _, e := range entries {
		var links []atomxml.Link
		if e.URL != "" {
			links = []atomxml.Link{{
				Rel:  "alternate",
				Href: e.URL,
				Type: "text/html",
			}}
		}

		categories := make([]atomxml.Category, 0, len(e.Tags))
		for _, tag := range e.Tags {
			categories = append(categories, atomxml.Category{Term: tag})
		}

		result = append(result, atomxml.Entry{
			ID:      e.ID,
			Title:   e.Title,
			Updated: e.Updated,
			Content: atomxml.ContentElement{
				Type:  "html",
				Value: e.ContentHTML,
			},
			Links:      links,
			Categories: categories,
		})
	}

	return result
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

// atomPaginationLinks builds self, alternate, and (optionally) next/previous
// links for the feed per RFC 5005 (Feed Paging and Archiving).
func atomPaginationLinks(r *http.Request, data feedData) []atomxml.Link {
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
	if data.BeforeID > 0 {
		firstPageHref := atomBaseHref(r, atomPath)
		links = append(links, atomxml.Link{
			Rel:  "previous",
			Href: firstPageHref,
			Type: "application/atom+xml",
		})
	}

	// When the cursor has been evicted from the ring buffer, the result is
	// empty despite requesting a non-first page. Include a "current" link
	// (RFC 5005 Section 2) so feed readers can recover.
	if data.BeforeID > 0 && len(data.Entries) == 0 {
		currentHref := atomBaseHref(r, atomPath)
		links = append(links, atomxml.Link{
			Rel:  "current",
			Href: currentHref,
			Type: "application/atom+xml",
		})
	}

	if data.LastItemID > 0 && len(data.Entries) == data.Limit {
		q := r.URL.Query()
		q.Set("before", strconv.FormatUint(data.LastItemID, 10))
		q.Set("limit", strconv.Itoa(data.Limit))
		nextHref := absURL(r, atomPath) + "?" + q.Encode()

		links = append(links, atomxml.Link{
			Rel:  "next",
			Href: nextHref,
			Type: "application/atom+xml",
		})
	}

	return links
}

// atomBaseHref builds the Atom feed URL without pagination params,
// preserving other query params (e.g., ?q= for search).
func atomBaseHref(r *http.Request, atomPath string) string {
	href := absURL(r, atomPath)
	if aq := r.URL.Query(); len(aq) > 0 {
		aq.Del("before")
		aq.Del("limit")

		if encoded := aq.Encode(); encoded != "" {
			href += "?" + encoded
		}
	}
	return href
}
