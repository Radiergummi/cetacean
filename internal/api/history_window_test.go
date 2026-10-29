package api

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	atomxml "github.com/radiergummi/cetacean/internal/api/atom"
	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/cache"
)

var windowBase = time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)

// windowedHistory holds one service entry a minute, "m0" at windowBase through
// "m5" five minutes later.
func windowedHistory() *cache.Cache {
	c := cache.New(nil)
	for i, name := range []string{"m0", "m1", "m2", "m3", "m4", "m5"} {
		c.History().Append(cache.HistoryEntry{
			Type:       cache.EventService,
			Action:     "update",
			ResourceID: name,
			Name:       name,
			Timestamp:  windowBase.Add(time.Duration(i) * time.Minute),
		})
	}

	return c
}

func windowQuery(from, to int) string {
	return url.Values{
		"since": {windowBase.Add(time.Duration(from) * time.Minute).Format(time.RFC3339)},
		"until": {windowBase.Add(time.Duration(to) * time.Minute).Format(time.RFC3339)},
	}.Encode()
}

// Since is exclusive and until inclusive, as get_events has them, and the next
// link keeps a page inside the window it started in.
func TestHandleHistory_TimeWindowPagesInsideIt(t *testing.T) {
	h := newTestHandlers(t, withCache(windowedHistory()))

	var all []string
	pages := 0
	for target := "/history?limit=2&" + windowQuery(1, 4); target != ""; pages++ {
		var names []string
		names, target = historyPage(t, h, target)
		all = append(all, names...)
	}

	if got := strings.Join(all, " "); got != "m4 m3 m2" || pages != 2 {
		t.Errorf("entries = %s over %d pages, want m4 m3 m2 over two", got, pages)
	}
}

func TestHandleHistory_RejectsAMalformedBound(t *testing.T) {
	h := newTestHandlers(t, withCache(windowedHistory()))

	for _, query := range []string{"since=yesterday", "until=2026-09-05"} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.HandleHistory(w, httptest.NewRequest("GET", "/history?"+query, nil))

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", w.Code)
			}

			var p problem.Detail
			if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
				t.Fatal(err)
			}

			param, _, _ := strings.Cut(query, "=")
			if !strings.HasSuffix(p.Type, "API016") || !strings.Contains(p.Detail, `"`+param+`"`) {
				t.Errorf("problem = %+v, want API016 naming %s", p, param)
			}
		})
	}
}

func TestHistoryCSVHonoursTheTimeWindow(t *testing.T) {
	router := newTestRouterWithCache(t, windowedHistory())

	body := getHistory(t, router, "/history.csv?"+windowQuery(1, 3))

	for name, want := range map[string]bool{"m1": false, "m2": true, "m3": true, "m4": false} {
		if got := strings.Contains(body, ","+name+","); got != want {
			t.Errorf("CSV holds %s = %t, want %t:\n%s", name, got, want, body)
		}
	}
}

// The feed's links carry the window, so a reader following next stays in it.
func TestHistoryAtomHonoursTheTimeWindow(t *testing.T) {
	router := newTestRouterWithCache(t, windowedHistory())

	body := getHistory(t, router, "/history.atom?limit=1&"+windowQuery(1, 3))

	var feed atomxml.Feed
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if len(feed.Entries) != 1 || !strings.HasSuffix(feed.Entries[0].Title, ": m3") {
		t.Fatalf("entries = %+v, want m3 alone", feed.Entries)
	}

	var next string
	for _, link := range feed.Links {
		if link.Rel == "next" {
			next = link.Href
		}
	}

	parsed, err := url.Parse(next)
	if err != nil || parsed.Query().Get("since") == "" || parsed.Query().Get("until") == "" {
		t.Errorf("next link %q does not carry the window", next)
	}
}

func TestHistoryFeedRejectsAMalformedBound(t *testing.T) {
	router := newTestRouterWithCache(t, windowedHistory())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/history.feed?since=now", nil))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// A windowed page echoes the bounds into its links; an unwindowed one echoes
// nothing beyond the cursor and stays compressed.
func TestHistoryFeedEchoingTheWindowIsNotCompressed(t *testing.T) {
	router := newTestRouterWithCache(t, windowedHistory())

	for target, want := range map[string]string{
		"/history.atom":                        "gzip",
		"/history.atom?" + windowQuery(-1, 10): "",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != want {
			t.Errorf("GET %s: Content-Encoding = %q, want %q", target, got, want)
		}
	}
}

// A JSON Feed reader refreshes from feed_url, so it has to keep the window.
func TestHistoryJSONFeedURLKeepsTheWindow(t *testing.T) {
	router := newTestRouterWithCache(t, windowedHistory())

	var feed struct {
		FeedURL     string `json:"feed_url"`
		HomePageURL string `json:"home_page_url"`
	}
	body := getHistory(t, router, "/history.feed?limit=1&"+windowQuery(1, 3))
	if err := json.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, href := range []string{feed.FeedURL, feed.HomePageURL} {
		parsed, err := url.Parse(href)
		if err != nil || parsed.Query().Get("since") == "" || parsed.Query().Get("until") == "" ||
			parsed.Query().Has("before") || parsed.Query().Has("limit") {
			t.Errorf("%q does not carry the window alone", href)
		}
	}
}
