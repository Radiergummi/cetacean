package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	"github.com/klauspost/compress/zstd"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
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
	r2.Header.Set(
		"If-Modified-Since",
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat),
	)
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

func TestEtagMatchStrongRejectsWeakValidators(t *testing.T) {
	const etag = `"abc123"`

	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"exact match", `"abc123"`, true},
		{"one of several", `"other", "abc123"`, true},
		{"wildcard", "*", true},
		{"no match", `"different"`, false},
		{"empty", "", false},
		// RFC 9110 §13.1.1: If-Match uses strong comparison, so a weak
		// validator never matches. etagMatch (used for If-None-Match, §13.1.2)
		// strips W/ and would wrongly accept this.
		{"weak validator", `W/"abc123"`, false},
		{"weak among strong", `"nope", W/"abc123"`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := etagMatchStrong(tc.header, etag); got != tc.want {
				t.Errorf("etagMatchStrong(%q, %q) = %v, want %v",
					tc.header, etag, got, tc.want)
			}
		})
	}
}

func TestEtagMatchStrongIgnoresCodingSuffix(t *testing.T) {
	// A client that read with Accept-Encoding: zstd holds "abc123-zstd" and
	// may send it as If-Match on a request negotiated as identity.
	if !etagMatchStrong(`"abc123-zstd"`, `"abc123"`) {
		t.Error("coding-suffixed validator did not match its base tag")
	}
	if !etagMatchStrong(`"abc123"`, `"abc123-gzip"`) {
		t.Error("base validator did not match a coding-suffixed current tag")
	}
}

func TestCompressedResponsesKeepConditionalCaching(t *testing.T) {
	router := newSeededTestRouter(t)

	first := httptest.NewRequest("GET", "/services", nil)
	first.Header.Set("Accept", "application/json")
	first.Header.Set("Accept-Encoding", "zstd")
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, first)

	if got := firstRec.Header().Get("Content-Encoding"); got != "zstd" {
		t.Fatalf("Content-Encoding = %q, want zstd", got)
	}

	etag := firstRec.Header().Get("ETag")
	if !strings.HasSuffix(strings.Trim(etag, `"`), "-zstd") {
		t.Errorf("ETag = %q, want a -zstd suffix", etag)
	}

	if !strings.Contains(strings.Join(firstRec.Header().Values("Vary"), ", "),
		"Accept-Encoding") {
		t.Error("Vary does not include Accept-Encoding")
	}

	// The body really is a zstd frame, and it decodes to the JSON an
	// identity read would have returned — trailing newline included, since
	// that byte has to live inside the frame rather than after it.
	decoded, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer decoded.Close()

	plain, err := decoded.DecodeAll(firstRec.Body.Bytes(), nil)
	if err != nil {
		t.Fatalf("response body is not a valid zstd frame: %v", err)
	}

	identity := httptest.NewRequest("GET", "/services", nil)
	identity.Header.Set("Accept", "application/json")
	identityRec := httptest.NewRecorder()
	router.ServeHTTP(identityRec, identity)

	if string(plain) != identityRec.Body.String() {
		t.Error("decompressed body differs from the identity representation")
	}

	second := httptest.NewRequest("GET", "/services", nil)
	second.Header.Set("Accept", "application/json")
	second.Header.Set("Accept-Encoding", "zstd")
	second.Header.Set("If-None-Match", etag)
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, second)

	if secondRec.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304 — compression must not cost us caching",
			secondRec.Code)
	}
	if secondRec.Body.Len() != 0 {
		t.Errorf("304 carried a %d-byte body", secondRec.Body.Len())
	}
}

// TestCompressedETagStillSatisfiesIfMatch guards the base hash staying
// coding-independent: precond hashes the identity representation, so an ETag
// hashed over compressed bytes could never match one, and every browser sends
// Accept-Encoding. There is a row per JSON helper because the preconditioned
// surface is split between them, and one row would leave the other unguarded.
func TestCompressedETagStillSatisfiesIfMatch(t *testing.T) {
	cases := []struct {
		name string
		// helper names the write path this row is here to cover, so a
		// failure says which half of the surface broke.
		helper      string
		getPath     string
		writeMethod string
		writePath   string
		wantStatus  int
	}{
		{
			"service detail", "writeCachedJSONTimed",
			"/services/svc1", "DELETE", "/services/svc1", http.StatusNoContent,
		},
		{
			"stack detail", "writeCachedJSONStatus",
			"/stacks/demo", "DELETE", "/stacks/demo", http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newSeededTestRouter(t)

			read := httptest.NewRequest("GET", tc.getPath, nil)
			read.Header.Set("Accept", "application/json")
			read.Header.Set("Accept-Encoding", "zstd")
			readRec := httptest.NewRecorder()
			router.ServeHTTP(readRec, read)

			if readRec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tc.getPath, readRec.Code)
			}
			// Guard the premise: an uncompressed read would make the rest of
			// this row pass for the wrong reason.
			if got := readRec.Header().Get("Content-Encoding"); got != "zstd" {
				t.Fatalf(
					"GET %s Content-Encoding = %q, want zstd — fixture body too small to compress",
					tc.getPath, got,
				)
			}

			etag := readRec.Header().Get("ETag")
			if !strings.HasSuffix(strings.Trim(etag, `"`), "-zstd") {
				t.Fatalf("ETag = %q, want a -zstd suffix", etag)
			}

			write := httptest.NewRequest(tc.writeMethod, tc.writePath, nil)
			write.Header.Set("Accept", "application/json")
			write.Header.Set("If-Match", etag)
			writeRec := httptest.NewRecorder()
			router.ServeHTTP(writeRec, write)

			if writeRec.Code != tc.wantStatus {
				t.Errorf(
					"%s %s = %d, want %d with the ETag its own compressed GET returned (%s)",
					tc.writeMethod, tc.writePath, writeRec.Code, tc.wantStatus, tc.helper,
				)
			}
		})
	}
}

// TestSearchIsNeverCompressed covers both search representations: the JSON
// handler echoes ?q= into the body and the feed titles itself with it, both
// beside ACL-filtered content, so both opt out. The cache is seeded until each
// response clears compressionThreshold, or either assertion would pass whether
// the opt-out were wired or not.
func TestSearchIsNeverCompressed(t *testing.T) {
	c := cache.New(nil)
	for i := range 60 {
		id := "web-" + strconv.Itoa(i)
		c.SetService(swarm.Service{
			ID:   id,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: id}},
		})
	}

	router := newTestRouterWithCache(t, c)

	cases := []struct {
		name   string
		path   string
		accept string
	}{
		{"json", "/search?q=web&limit=0", "application/json"},
		{"atom", "/search?q=web", "application/atom+xml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			req.Header.Set("Accept", tc.accept)
			req.Header.Set("Accept-Encoding", "zstd")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Encoding"); got != "" {
				t.Errorf("Content-Encoding = %q, want empty for %s", got, tc.path)
			}
			if rec.Body.Len() <= compressionThreshold {
				t.Errorf(
					"body is %d bytes, under the %d-byte threshold — this case proves nothing",
					rec.Body.Len(), compressionThreshold,
				)
			}
		})
	}
}

func TestSSEStillStreamsUnderCompression(t *testing.T) {
	// No ResponseWriter is wrapped anywhere, so the w.(http.Flusher) assertions
	// in sse/broadcaster.go, log_handlers.go and metricsstream.go keep working.
	// The broadcaster is wired onto the handlers because streamList reads
	// theirs, and a nil one panics before the stream opens.
	broadcaster := sse.NewBroadcaster(0, noopErrorWriter, nil)
	t.Cleanup(broadcaster.Close)

	router := newTestRouterWithCache(t, cache.New(nil), withBroadcaster(broadcaster))
	req := httptest.NewRequest("GET", "/nodes", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "zstd")
	rec := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(req.Context(), 200*time.Millisecond)
	defer cancel()
	router.ServeHTTP(rec, req.WithContext(ctx))

	// Guard the premise: without this, a negotiation that fell through to the
	// SPA shell would satisfy every assertion below.
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream — the SSE path was not taken", got)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty — SSE must not be compressed", got)
	}
	if rec.Code == http.StatusInternalServerError {
		t.Error("SSE returned 500 — a ResponseWriter wrapper broke http.Flusher")
	}
}

// TestAtomFeedsAreCompressed covers the other body-producing ETag helper:
// writeCachedAtom renders XML, which compresses better than anything else
// the API serves, and it accumulates its coding onto the Vary it already
// writes rather than replacing it.
func TestAtomFeedsAreCompressed(t *testing.T) {
	c := cache.New(nil)
	for i := range 60 {
		id := "web-" + strconv.Itoa(i)
		c.SetService(swarm.Service{
			ID:   id,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: id}},
		})
	}

	router := newTestRouterWithCache(t, c)

	req := httptest.NewRequest("GET", "/history", nil)
	req.Header.Set("Accept", "application/atom+xml")
	req.Header.Set("Accept-Encoding", "zstd")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Encoding"); got != "zstd" {
		t.Fatalf("Content-Encoding = %q, want zstd", got)
	}
	if etag := rec.Header().Get("ETag"); !strings.HasSuffix(strings.Trim(etag, `"`), "-zstd") {
		t.Errorf("ETag = %q, want a -zstd suffix", etag)
	}

	vary := strings.Join(rec.Header().Values("Vary"), ", ")
	for _, want := range []string{"Authorization, Cookie", "Accept-Encoding"} {
		if !strings.Contains(vary, want) {
			t.Errorf("Vary = %q, want it to include %q", vary, want)
		}
	}
}

// TestCodedETagSuffixesAreStrippable holds codedETag and knownCodingSuffixes
// together: the precondition path works only because stripCodingSuffix undoes
// exactly what codedETag did. It iterates compressibleEncodings rather than a
// literal pair, so a third coding is covered the moment it exists.
func TestCodedETagSuffixesAreStrippable(t *testing.T) {
	base := computeETag([]byte("a representation"))

	for _, coding := range compressibleEncodings {
		t.Run(coding.String(), func(t *testing.T) {
			tagged := codedETag(base, coding)

			if tagged == base {
				t.Fatalf("codedETag(%q, %v) left the tag unchanged", base, coding)
			}
			if !strings.HasPrefix(tagged, `"`) || !strings.HasSuffix(tagged, `"`) {
				t.Errorf("tag %q is not quoted — the suffix belongs inside the quotes", tagged)
			}
			if got := stripCodingSuffix(strings.Trim(tagged, `"`)); got != strings.Trim(base, `"`) {
				t.Errorf("stripCodingSuffix(%q) = %q, want %q",
					tagged, got, strings.Trim(base, `"`))
			}
			if !etagMatchStrong(tagged, base) {
				t.Error("a coding-suffixed validator no longer satisfies If-Match on its base")
			}
		})
	}

	if got := codedETag(base, EncodingIdentity); got != base {
		t.Errorf("codedETag(%q, identity) = %q, want it untouched", base, got)
	}
}
