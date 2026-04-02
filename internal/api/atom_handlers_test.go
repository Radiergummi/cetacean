package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	atomxml "github.com/radiergummi/cetacean/internal/api/atom"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestWriteCachedAtom(t *testing.T) {
	feed := atomxml.Feed{
		Title:   "Test Feed",
		ID:      "tag:example.com,2026:/history",
		Updated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	t.Run("sets Content-Type, ETag, and Cache-Control", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/history", nil)

		writeCachedAtom(rec, req, feed)

		if ct := rec.Header().Get("Content-Type"); ct != "application/atom+xml;charset=utf-8" {
			t.Errorf("Content-Type = %q, want application/atom+xml;charset=utf-8", ct)
		}

		if etag := rec.Header().Get("ETag"); etag == "" {
			t.Error("expected ETag header to be set")
		}

		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", cc)
		}

		if vary := rec.Header().Get("Vary"); vary != "Authorization, Cookie" {
			t.Errorf("Vary = %q, want %q", vary, "Authorization, Cookie")
		}

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("returns 304 on matching If-None-Match", func(t *testing.T) {
		// First request to get the ETag.
		rec1 := httptest.NewRecorder()
		req1 := httptest.NewRequest("GET", "/history", nil)
		writeCachedAtom(rec1, req1, feed)
		etag := rec1.Header().Get("ETag")

		// Second request with matching If-None-Match.
		rec2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/history", nil)
		req2.Header.Set("If-None-Match", etag)
		writeCachedAtom(rec2, req2, feed)

		if rec2.Code != http.StatusNotModified {
			t.Errorf("status = %d, want %d", rec2.Code, http.StatusNotModified)
		}

		if rec2.Body.Len() != 0 {
			t.Errorf("expected empty body on 304, got %d bytes", rec2.Body.Len())
		}
	})
}

func TestFeedID(t *testing.T) {
	t.Run("uses Host header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history", nil)
		req.Host = "swarm.example.com"

		got := feedID(req)
		want := "tag:swarm.example.com,2026:/history"

		if got != want {
			t.Errorf("feedID = %q, want %q", got, want)
		}
	})

	t.Run("includes base path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history", nil)
		req.Host = "swarm.example.com"
		ctx := context.WithValue(req.Context(), basePathKey, "/cetacean")
		req = req.WithContext(ctx)

		got := feedID(req)
		want := "tag:swarm.example.com,2026:/cetacean/history"

		if got != want {
			t.Errorf("feedID = %q, want %q", got, want)
		}
	})
}

func TestHistoryToEntries(t *testing.T) {
	req := httptest.NewRequest("GET", "/history", nil)
	ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	t.Run("converts entries with Summary", func(t *testing.T) {
		entries := []cache.HistoryEntry{{
			ID:         42,
			Timestamp:  ts,
			Type:       cache.EventService,
			Action:     "update",
			ResourceID: "svc-abc",
			Name:       "web",
			Summary:    "scaled from 3 to 5 replicas",
		}}

		result := historyToEntries(req, entries)

		if len(result) != 1 {
			t.Fatalf("got %d entries, want 1", len(result))
		}

		e := result[0]

		if e.Content.Value != "scaled from 3 to 5 replicas" {
			t.Errorf("content = %q, want %q", e.Content.Value, "scaled from 3 to 5 replicas")
		}

		if e.Content.Type != "text" {
			t.Errorf("content type = %q, want text", e.Content.Type)
		}

		if e.Title != "scaled from 3 to 5 replicas" {
			t.Errorf("title = %q, want %q", e.Title, "scaled from 3 to 5 replicas")
		}

		if len(e.Categories) != 1 || e.Categories[0].Term != "service" {
			t.Errorf("categories = %v, want [{Term:service}]", e.Categories)
		}
	})

	t.Run("falls back to Action Name when Summary is empty", func(t *testing.T) {
		entries := []cache.HistoryEntry{{
			ID:         43,
			Timestamp:  ts,
			Type:       cache.EventNode,
			Action:     "create",
			ResourceID: "node-xyz",
			Name:       "worker-1",
		}}

		result := historyToEntries(req, entries)

		if len(result) != 1 {
			t.Fatalf("got %d entries, want 1", len(result))
		}

		if result[0].Content.Value != "create worker-1" {
			t.Errorf("content = %q, want %q", result[0].Content.Value, "create worker-1")
		}
	})

	t.Run("sets correct category term", func(t *testing.T) {
		entries := []cache.HistoryEntry{{
			ID:         44,
			Timestamp:  ts,
			Type:       cache.EventNetwork,
			Action:     "remove",
			ResourceID: "net-123",
			Name:       "overlay-net",
			Summary:    "removed network",
		}}

		result := historyToEntries(req, entries)

		if len(result) != 1 {
			t.Fatalf("got %d entries, want 1", len(result))
		}

		if result[0].Categories[0].Term != "network" {
			t.Errorf("category term = %q, want network", result[0].Categories[0].Term)
		}
	})
}

func TestParseAtomPagination(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history", nil)
		beforeID, limit := parseAtomPagination(req)

		if beforeID != 0 {
			t.Errorf("beforeID = %d, want 0", beforeID)
		}

		if limit != 50 {
			t.Errorf("limit = %d, want 50", limit)
		}
	})

	t.Run("parses values", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history?before=100&limit=25", nil)
		beforeID, limit := parseAtomPagination(req)

		if beforeID != 100 {
			t.Errorf("beforeID = %d, want 100", beforeID)
		}

		if limit != 25 {
			t.Errorf("limit = %d, want 25", limit)
		}
	})

	t.Run("caps limit at 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history?limit=500", nil)
		_, limit := parseAtomPagination(req)

		if limit != 200 {
			t.Errorf("limit = %d, want 200", limit)
		}
	})
}

func TestResourcePath(t *testing.T) {
	cases := []struct {
		typ  cache.EventType
		id   string
		want string
	}{
		{cache.EventNode, "abc", "/nodes/abc"},
		{cache.EventService, "svc1", "/services/svc1"},
		{cache.EventTask, "t1", "/tasks/t1"},
		{cache.EventConfig, "c1", "/configs/c1"},
		{cache.EventSecret, "s1", "/secrets/s1"},
		{cache.EventNetwork, "n1", "/networks/n1"},
		{cache.EventVolume, "v1", "/volumes/v1"},
		{cache.EventStack, "mystack", "/stacks/mystack"},
		{"unknown", "x", ""},
	}

	for _, tc := range cases {
		got := resourcePath(tc.typ, tc.id)
		if got != tc.want {
			t.Errorf("resourcePath(%q, %q) = %q, want %q", tc.typ, tc.id, got, tc.want)
		}
	}
}

func TestPaginationLinks(t *testing.T) {
	t.Run("self and alternate only when not full page", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history", nil)
		entries := make([]cache.HistoryEntry, 3)
		links := paginationLinks(req, entries, 0, 50)

		var hasNext, hasPrevious bool
		for _, l := range links {
			if l.Rel == "next" {
				hasNext = true
			}

			if l.Rel == "previous" {
				hasPrevious = true
			}
		}

		if hasNext {
			t.Error("expected no next link when len(entries) < limit")
		}

		if hasPrevious {
			t.Error("expected no previous link on first page (beforeID=0)")
		}
	})

	t.Run("includes next when page is full", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history", nil)
		entries := make([]cache.HistoryEntry, 50)
		entries[49].ID = 42
		links := paginationLinks(req, entries, 0, 50)

		var hasNext bool
		for _, l := range links {
			if l.Rel == "next" {
				hasNext = true
			}
		}

		if !hasNext {
			t.Error("expected next link when len(entries) == limit")
		}
	})

	t.Run("alternate link does not contain pagination params", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history?before=100&limit=25", nil)
		entries := make([]cache.HistoryEntry, 3)
		links := paginationLinks(req, entries, 100, 25)

		for _, l := range links {
			if l.Rel != "alternate" {
				continue
			}

			if l.Href != "/history" {
				t.Errorf("alternate href = %q, want /history (no query params)", l.Href)
			}
		}
	})

	t.Run("includes previous link on non-first page", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/history?before=100&limit=25", nil)
		entries := make([]cache.HistoryEntry, 3)
		links := paginationLinks(req, entries, 100, 25)

		var previousHref string
		for _, l := range links {
			if l.Rel == "previous" {
				previousHref = l.Href
			}
		}

		if previousHref == "" {
			t.Fatal("expected previous link on non-first page (beforeID > 0)")
		}

		if previousHref != "/history" {
			t.Errorf("previous href = %q, want /history (subscription document)", previousHref)
		}
	})

	t.Run("next link preserves existing query params", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/search?q=myservice", nil)
		entries := make([]cache.HistoryEntry, 50)
		entries[49].ID = 42
		links := paginationLinks(req, entries, 0, 50)

		var nextHref string
		for _, l := range links {
			if l.Rel == "next" {
				nextHref = l.Href
			}
		}

		if nextHref == "" {
			t.Fatal("expected next link")
		}

		if !strings.Contains(nextHref, "q=myservice") {
			t.Errorf("next href %q should preserve q param", nextHref)
		}

		if !strings.Contains(nextHref, "before=42") {
			t.Errorf("next href %q should contain before=42", nextHref)
		}

		if !strings.Contains(nextHref, "limit=50") {
			t.Errorf("next href %q should contain limit=50", nextHref)
		}
	})
}

func TestPaginationLinks_StaleCursorIncludesCurrentLink(t *testing.T) {
	req := httptest.NewRequest("GET", "/history?before=9999&limit=50", nil)
	entries := []cache.HistoryEntry{} // empty — cursor was evicted

	links := paginationLinks(req, entries, 9999, 50)

	var currentHref string
	for _, l := range links {
		if l.Rel == "current" {
			currentHref = l.Href
		}
	}

	if currentHref == "" {
		t.Fatal("expected current link when beforeID > 0 and entries are empty (stale cursor)")
	}

	if currentHref != "/history" {
		t.Errorf("current href = %q, want /history (no pagination params)", currentHref)
	}
}

func TestPaginationLinks_StaleCursorPreservesQueryParams(t *testing.T) {
	req := httptest.NewRequest("GET", "/search?q=myservice&before=9999&limit=50", nil)
	entries := []cache.HistoryEntry{} // empty — cursor was evicted

	links := paginationLinks(req, entries, 9999, 50)

	var currentHref string
	for _, l := range links {
		if l.Rel == "current" {
			currentHref = l.Href
		}
	}

	if currentHref == "" {
		t.Fatal("expected current link for stale cursor")
	}

	if !strings.Contains(currentHref, "q=myservice") {
		t.Errorf("current href %q should preserve q param", currentHref)
	}

	if strings.Contains(currentHref, "before=") || strings.Contains(currentHref, "limit=") {
		t.Errorf("current href %q should not contain pagination params", currentHref)
	}
}

func TestHistoryUpdated(t *testing.T) {
	t.Run("returns first entry timestamp when entries exist", func(t *testing.T) {
		ts := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
		entries := []cache.HistoryEntry{{Timestamp: ts}}

		got := historyUpdated(entries)
		if !got.Equal(ts) {
			t.Errorf("historyUpdated = %v, want %v", got, ts)
		}
	})

	t.Run("returns stable epoch when entries are empty", func(t *testing.T) {
		got := historyUpdated(nil)

		if !got.Equal(emptyFeedEpoch) {
			t.Errorf("historyUpdated = %v, want %v", got, emptyFeedEpoch)
		}
	})
}
