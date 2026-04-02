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

	handler := contentNegotiated(jsonH, atomH, spa)

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
		handler := contentNegotiated(jsonH, nil, spa)
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeAtom)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusNotAcceptable {
			t.Errorf("got %d, want 406", rec.Code)
		}
	})
}

func TestAtomLinkHeader(t *testing.T) {
	jsonH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("json"))
	})
	atomH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("atom"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	t.Run("JSON response includes atom Link header when atom handler exists", func(t *testing.T) {
		handler := contentNegotiated(jsonH, atomH, spa)
		req := httptest.NewRequest("GET", "/services", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, `rel="alternate"`) {
			t.Errorf("expected alternate Link header, got %q", links)
		}
		if !strings.Contains(links, `type="application/atom+xml"`) {
			t.Errorf("expected atom+xml type in Link header, got %q", links)
		}
		if !strings.Contains(links, "/services.atom") {
			t.Errorf("expected /services.atom href in Link header, got %q", links)
		}
	})

	t.Run("JSON response has no atom Link when atom handler is nil", func(t *testing.T) {
		handler := contentNegotiated(jsonH, nil, spa)
		req := httptest.NewRequest("GET", "/cluster", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		for _, link := range rec.Header().Values("Link") {
			if strings.Contains(link, "atom") {
				t.Errorf("expected no atom Link header, got %q", link)
			}
		}
	})

	t.Run("atom Link preserves query string", func(t *testing.T) {
		handler := contentNegotiated(jsonH, atomH, spa)
		req := httptest.NewRequest("GET", "/search?q=web&limit=10", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, "/search.atom?q=web&limit=10") {
			t.Errorf("expected query params preserved in atom Link, got %q", links)
		}
	})

	t.Run("contentNegotiatedWithSSE also adds atom Link on JSON", func(t *testing.T) {
		sseH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		handler := contentNegotiatedWithSSE(jsonH, sseH, atomH, spa)
		req := httptest.NewRequest("GET", "/nodes", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, "/nodes.atom") {
			t.Errorf("expected /nodes.atom in Link header, got %q", links)
		}
	})
}
