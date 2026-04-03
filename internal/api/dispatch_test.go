package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContentNegotiatedAtom(t *testing.T) {
	jsonH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("json"))
	})
	atomH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("atom"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("spa"))
	})

	handler := contentNegotiated(jsonH, feedHandlers{atom: atomH}, spa)

	t.Run("Atom content type dispatches to atom handler", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeAtom)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Body.String() != "atom" {
			t.Errorf("got %q, want %q", rec.Body.String(), "atom")
		}
	})

	t.Run("nil atom handler returns 406", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{}, spa)
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeAtom)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusNotAcceptable {
			t.Errorf("got %d, want 406", rec.Code)
		}
	})
}

func TestContentNegotiatedJSONFeed(t *testing.T) {
	jsonH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("json"))
	})
	feedH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("jsonfeed"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("spa"))
	})

	handler := contentNegotiated(jsonH, feedHandlers{jsonFeed: feedH}, spa)

	t.Run("JSON Feed content type dispatches to JSON Feed handler", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeJSONFeed)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Body.String() != "jsonfeed" {
			t.Errorf("got %q, want %q", rec.Body.String(), "jsonfeed")
		}
	})

	t.Run("nil JSON Feed handler returns 406", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{}, spa)
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeJSONFeed)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusNotAcceptable {
			t.Errorf("got %d, want 406", rec.Code)
		}
	})
}

func TestFeedLinkHeaders(t *testing.T) {
	jsonH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("json"))
	})
	atomH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("atom"))
	})
	feedH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("jsonfeed"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	t.Run("JSON response includes atom Link header when atom handler exists", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{atom: atomH}, spa)
		req := httptest.NewRequest("GET", "/services", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, `type="application/atom+xml"`) {
			t.Errorf("expected atom+xml type in Link header, got %q", links)
		}
		if !strings.Contains(links, "/services.atom") {
			t.Errorf("expected /services.atom href in Link header, got %q", links)
		}
	})

	t.Run("JSON response includes JSON Feed Link header when handler exists", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{jsonFeed: feedH}, spa)
		req := httptest.NewRequest("GET", "/nodes", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, `type="application/feed+json"`) {
			t.Errorf("expected feed+json type in Link header, got %q", links)
		}
		if !strings.Contains(links, "/nodes.feed") {
			t.Errorf("expected /nodes.feed href in Link header, got %q", links)
		}
	})

	t.Run("both feed Links present when both handlers exist", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{atom: atomH, jsonFeed: feedH}, spa)
		req := httptest.NewRequest("GET", "/tasks", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, "/tasks.atom") {
			t.Errorf("expected /tasks.atom in Link header, got %q", links)
		}
		if !strings.Contains(links, "/tasks.feed") {
			t.Errorf("expected /tasks.feed in Link header, got %q", links)
		}
	})

	t.Run("no feed Links when no handlers", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{}, spa)
		req := httptest.NewRequest("GET", "/cluster", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		for _, link := range rec.Header().Values("Link") {
			if strings.Contains(link, "atom") || strings.Contains(link, "feed+json") {
				t.Errorf("expected no feed Link header, got %q", link)
			}
		}
	})

	t.Run("feed Links preserve query string", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{atom: atomH, jsonFeed: feedH}, spa)
		req := httptest.NewRequest("GET", "/search?q=web&limit=10", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, "/search.atom?q=web&limit=10") {
			t.Errorf("expected query params preserved in atom Link, got %q", links)
		}
		if !strings.Contains(links, "/search.feed?q=web&limit=10") {
			t.Errorf("expected query params preserved in feed Link, got %q", links)
		}
	})

	t.Run("contentNegotiatedWithSSE also adds feed Links on JSON", func(t *testing.T) {
		sseH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		handler := contentNegotiatedWithSSE(
			jsonH,
			sseH,
			feedHandlers{atom: atomH, jsonFeed: feedH},
			spa,
		)
		req := httptest.NewRequest("GET", "/nodes", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, "/nodes.atom") {
			t.Errorf("expected /nodes.atom in Link header, got %q", links)
		}
		if !strings.Contains(links, "/nodes.feed") {
			t.Errorf("expected /nodes.feed in Link header, got %q", links)
		}
	})
}
