package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	r1 := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	w1 := httptest.NewRecorder()
	writeCachedJSON(w1, r1, data)

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
	r2 := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	r2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	writeCachedJSON(w2, r2, data)

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

	r := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	r.Header.Set("If-None-Match", `"stale-etag-value"`)
	w := httptest.NewRecorder()
	writeCachedJSON(w, r, data)

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

func TestETagMatch(t *testing.T) {
	tests := []struct {
		name   string
		header string
		etag   string
		want   bool
	}{
		{"empty header", "", `"abc"`, false},
		{"exact match", `"abc"`, `"abc"`, true},
		{"no match", `"xyz"`, `"abc"`, false},
		{"wildcard", "*", `"abc"`, true},
		{"weak etag match", `W/"abc"`, `"abc"`, true},
		{"multi-value first", `"abc", "def"`, `"abc"`, true},
		{"multi-value second", `"abc", "def"`, `"def"`, true},
		{"multi-value no match", `"abc", "def"`, `"ghi"`, false},
		{"multi-value with weak", `W/"abc", "def"`, `"abc"`, true},
		{"multi-value with spaces", `"abc" , "def" , "ghi"`, `"def"`, true},
		{"weak in multi-value", `"abc", W/"def", "ghi"`, `"def"`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := etagMatch(tt.header, tt.etag); got != tt.want {
				t.Errorf("etagMatch(%q, %q) = %v, want %v", tt.header, tt.etag, got, tt.want)
			}
		})
	}
}

func TestETagConditionalMultiValue(t *testing.T) {
	data := map[string]string{"status": "ok"}

	// Get the ETag for this data.
	r1 := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	w1 := httptest.NewRecorder()
	writeCachedJSON(w1, r1, data)
	etag := w1.Header().Get("ETag")

	// Send If-None-Match with multiple ETags including the correct one.
	r2 := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	r2.Header.Set("If-None-Match", `"stale", `+etag+`, "other"`)
	w2 := httptest.NewRecorder()
	writeCachedJSON(w2, r2, data)

	if w2.Code != http.StatusNotModified {
		t.Fatalf("expected 304 with multi-value If-None-Match, got %d", w2.Code)
	}
}

func TestETagConditionalWeak(t *testing.T) {
	data := map[string]string{"status": "ok"}

	// Get the ETag.
	r1 := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	w1 := httptest.NewRecorder()
	writeCachedJSON(w1, r1, data)
	etag := w1.Header().Get("ETag")

	// Send weak version of the same ETag.
	r2 := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	r2.Header.Set("If-None-Match", "W/"+etag)
	w2 := httptest.NewRecorder()
	writeCachedJSON(w2, r2, data)

	if w2.Code != http.StatusNotModified {
		t.Fatalf("expected 304 with weak ETag, got %d", w2.Code)
	}
}

func TestETagConditionalWildcard(t *testing.T) {
	data := map[string]string{"status": "ok"}

	r := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	r.Header.Set("If-None-Match", "*")
	w := httptest.NewRecorder()
	writeCachedJSON(w, r, data)

	if w.Code != http.StatusNotModified {
		t.Fatalf("expected 304 with wildcard If-None-Match, got %d", w.Code)
	}
}

func TestLastModifiedHeader(t *testing.T) {
	data := map[string]string{"status": "ok"}
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	r := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	w := httptest.NewRecorder()
	writeCachedJSONTimed(w, r, data, ts)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	lm := w.Header().Get("Last-Modified")
	if lm == "" {
		t.Fatal("expected Last-Modified header")
	}

	want := "Sun, 15 Jun 2025 10:30:00 GMT"
	if lm != want {
		t.Fatalf("Last-Modified = %q, want %q", lm, want)
	}
}

func TestLastModifiedZero(t *testing.T) {
	data := map[string]string{"status": "ok"}

	r := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	w := httptest.NewRecorder()
	writeCachedJSONTimed(w, r, data, time.Time{})

	if lm := w.Header().Get("Last-Modified"); lm != "" {
		t.Fatalf("expected no Last-Modified for zero time, got %q", lm)
	}
}

func TestIfModifiedSince_NotModified(t *testing.T) {
	data := map[string]string{"status": "ok"}
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	r := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	r.Header.Set("If-Modified-Since", ts.Format(http.TimeFormat))
	w := httptest.NewRecorder()
	writeCachedJSONTimed(w, r, data, ts)

	if w.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected empty body on 304, got %d bytes", w.Body.Len())
	}
}

func TestIfModifiedSince_Modified(t *testing.T) {
	data := map[string]string{"status": "ok"}
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	older := time.Date(2025, 6, 14, 10, 30, 0, 0, time.UTC)

	r := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	r.Header.Set("If-Modified-Since", older.Format(http.TimeFormat))
	w := httptest.NewRecorder()
	writeCachedJSONTimed(w, r, data, ts)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Fatal("expected non-empty body when resource is newer")
	}
}

func TestIfNoneMatchTakesPrecedenceOverIfModifiedSince(t *testing.T) {
	data := map[string]string{"status": "ok"}
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	// First, get the ETag.
	r1 := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	w1 := httptest.NewRecorder()
	writeCachedJSONTimed(w1, r1, data, ts)
	etag := w1.Header().Get("ETag")

	// Send both If-None-Match (matching) and If-Modified-Since (stale date).
	// ETag should take precedence → 304.
	r2 := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	r2.Header.Set("If-None-Match", etag)
	r2.Header.Set("If-Modified-Since", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
	w2 := httptest.NewRecorder()
	writeCachedJSONTimed(w2, r2, data, ts)

	if w2.Code != http.StatusNotModified {
		t.Fatalf("expected 304 (ETag precedence), got %d", w2.Code)
	}
}

func TestIfNoneMatchMismatchOverridesIfModifiedSince(t *testing.T) {
	data := map[string]string{"status": "ok"}
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	// Send non-matching If-None-Match + matching If-Modified-Since.
	// ETag takes precedence → mismatch → 200 (If-Modified-Since is ignored).
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/test", nil)
	r.Header.Set("If-None-Match", `"wrong-etag"`)
	r.Header.Set("If-Modified-Since", ts.Format(http.TimeFormat))
	w := httptest.NewRecorder()
	writeCachedJSONTimed(w, r, data, ts)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (ETag mismatch overrides), got %d", w.Code)
	}
}
