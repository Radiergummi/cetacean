package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNegotiate(t *testing.T) {
	// Helper: runs a request through the negotiate middleware and returns
	// the resolved ContentType and the path seen by the inner handler.
	run := func(path string, accept string) (ContentType, string) {
		var ct ContentType
		var innerPath string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ct = ContentTypeFromContext(r.Context())
			innerPath = r.URL.Path
		})

		req := httptest.NewRequest("GET", path, nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		rec := httptest.NewRecorder()
		negotiate(inner).ServeHTTP(rec, req)
		return ct, innerPath
	}

	t.Run("extension .json strips suffix and returns JSON", func(t *testing.T) {
		ct, path := run("/services.json", "")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
		if path != "/services" {
			t.Errorf("got path %q, want /services", path)
		}
	})

	t.Run("extension .html strips suffix and returns HTML", func(t *testing.T) {
		ct, path := run("/services.html", "")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
		if path != "/services" {
			t.Errorf("got path %q, want /services", path)
		}
	})

	t.Run("Accept application/json", func(t *testing.T) {
		ct, _ := run("/services", "application/json")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("Accept application/vnd.cetacean.v1+json", func(t *testing.T) {
		ct, _ := run("/services", "application/vnd.cetacean.v1+json")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("Accept text/html browser default", func(t *testing.T) {
		ct, _ := run("/services", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	t.Run("Accept text/event-stream", func(t *testing.T) {
		ct, _ := run("/events", "text/event-stream")
		if ct != ContentTypeSSE {
			t.Errorf("got %v, want SSE", ct)
		}
	})

	t.Run("no Accept header defaults to JSON", func(t *testing.T) {
		ct, _ := run("/services", "")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("Accept */* defaults to JSON", func(t *testing.T) {
		ct, _ := run("/services", "*/*")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("extension overrides Accept header", func(t *testing.T) {
		ct, path := run("/services.html", "application/json")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
		if path != "/services" {
			t.Errorf("got path %q, want /services", path)
		}
	})

	t.Run("quality value parsing prefers higher q", func(t *testing.T) {
		ct, _ := run("/services", "text/html;q=0.9, application/json;q=1.0")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("quality value parsing html wins", func(t *testing.T) {
		ct, _ := run("/services", "application/json;q=0.5, text/html;q=0.9")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	t.Run("Vary header is set", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		req := httptest.NewRequest("GET", "/services", nil)
		rec := httptest.NewRecorder()
		negotiate(inner).ServeHTTP(rec, req)
		if v := rec.Header().Get("Vary"); v != "Accept" {
			t.Errorf("Vary header = %q, want Accept", v)
		}
	})

	t.Run("application/xhtml+xml returns HTML", func(t *testing.T) {
		ct, _ := run("/services", "application/xhtml+xml")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	t.Run("path with dot but not known extension is unchanged", func(t *testing.T) {
		ct, path := run("/services/my.service", "application/json")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
		if path != "/services/my.service" {
			t.Errorf("got path %q, want /services/my.service", path)
		}
	})

	t.Run("ContentTypeFromContext default is JSON", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		ct := ContentTypeFromContext(req.Context())
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	// --- Partial wildcard support ---

	t.Run("partial wildcard text/* matches HTML", func(t *testing.T) {
		ct, _ := run("/services", "text/*")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	t.Run("partial wildcard application/* matches JSON", func(t *testing.T) {
		ct, _ := run("/services", "application/*")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	// --- Specificity ---

	t.Run("specificity: exact match beats partial wildcard at same q", func(t *testing.T) {
		// text/* and application/json both q=1.0, but application/json is exact (specificity 3)
		// while text/* is partial (specificity 2).
		ct, _ := run("/services", "text/*, application/json")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("specificity: higher q wins over higher specificity", func(t *testing.T) {
		// text/html is exact (specificity 3, q=0.9); text/* is partial (specificity 2, q=1.0).
		// Higher q wins, so text/* matches. text/* matches text/html first in our preference order.
		ct, _ := run("/services", "text/html;q=0.9, text/*;q=1.0")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	// --- Unsupported types → 406 at middleware level ---

	t.Run("application/xml alone returns 406", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("inner handler should not be called for unsupported type")
		})
		req := httptest.NewRequest("GET", "/services", nil)
		req.Header.Set("Accept", "application/xml")
		rec := httptest.NewRecorder()
		negotiate(inner).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotAcceptable {
			t.Errorf("status=%d, want 406", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
			t.Errorf("content-type=%q, want application/problem+json", ct)
		}
	})

	t.Run("text/plain alone returns 406", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("inner handler should not be called for unsupported type")
		})
		req := httptest.NewRequest("GET", "/services", nil)
		req.Header.Set("Accept", "text/plain")
		rec := httptest.NewRecorder()
		negotiate(inner).ServeHTTP(rec, req)
		if rec.Code != http.StatusNotAcceptable {
			t.Errorf("status=%d, want 406", rec.Code)
		}
	})

	t.Run("application/xml with */* fallback returns JSON", func(t *testing.T) {
		ct, _ := run("/services", "application/xml, */*;q=0.1")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("unsupported type with higher q still picks only match", func(t *testing.T) {
		// application/xml;q=1.0 is unsupported; application/json;q=0.5 is our only match.
		ct, _ := run("/services", "application/xml;q=1.0, application/json;q=0.5")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	// --- Malformed headers ---

	t.Run("malformed: empty commas", func(t *testing.T) {
		ct, _ := run("/services", ",,")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for all-empty ranges)", ct)
		}
	})

	t.Run("malformed: broken q value", func(t *testing.T) {
		// ";q=broken" has no valid media type before the semicolon — it's just "".
		ct, _ := run("/services", ";q=broken")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for malformed)", ct)
		}
	})

	t.Run("malformed: bare slash", func(t *testing.T) {
		ct, _ := run("/services", "/")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for malformed)", ct)
		}
	})

	t.Run("malformed: no subtype", func(t *testing.T) {
		ct, _ := run("/services", "text")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for malformed)", ct)
		}
	})

	t.Run("malformed: empty subtype after slash", func(t *testing.T) {
		ct, _ := run("/services", "text/")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for malformed)", ct)
		}
	})

	// --- ContentType String ---

	t.Run("ContentTypeUnsupported String", func(t *testing.T) {
		if s := ContentTypeUnsupported.String(); s != "Unsupported" {
			t.Errorf("got %q, want Unsupported", s)
		}
	})
}
