package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestETagGeneration(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	etag := computeETag(body)

	if etag == "" {
		t.Fatal("etag should not be empty")
	}
	// Must be quoted per HTTP spec
	if etag[0] != '"' || etag[len(etag)-1] != '"' {
		t.Fatalf("etag should be quoted, got %s", etag)
	}
	// Deterministic: same input → same output
	if etag2 := computeETag(body); etag != etag2 {
		t.Fatalf("etag not deterministic: %s != %s", etag, etag2)
	}
	// Different input → different output
	if etag3 := computeETag([]byte(`{"other":true}`)); etag == etag3 {
		t.Fatal("different inputs should produce different etags")
	}
}

func TestETagConditionalRequest(t *testing.T) {
	data := map[string]string{"status": "ok"}

	// First request: should get 200 + ETag header
	r1 := httptest.NewRequest("GET", "/test", nil)
	w1 := httptest.NewRecorder()
	writeJSONWithETag(w1, r1, data)

	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w1.Code)
	}
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header on first request")
	}
	if w1.Body.Len() == 0 {
		t.Fatal("expected non-empty body on first request")
	}

	// Second request with matching If-None-Match: should get 304 + empty body
	r2 := httptest.NewRequest("GET", "/test", nil)
	r2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	writeJSONWithETag(w2, r2, data)

	if w2.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", w2.Code)
	}
	if w2.Body.Len() != 0 {
		t.Fatalf("expected empty body on 304, got %d bytes", w2.Body.Len())
	}
	// ETag header should still be present on 304
	if w2.Header().Get("ETag") == "" {
		t.Fatal("expected ETag header on 304 response")
	}
}

func TestETagMismatch(t *testing.T) {
	data := map[string]string{"status": "ok"}

	r := httptest.NewRequest("GET", "/test", nil)
	r.Header.Set("If-None-Match", `"stale-etag-value"`)
	w := httptest.NewRecorder()
	writeJSONWithETag(w, r, data)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Fatal("expected non-empty body when ETag doesn't match")
	}
	if w.Header().Get("ETag") == "" {
		t.Fatal("expected ETag header")
	}
}
