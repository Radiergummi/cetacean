package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestStaticBodyCompressesEachCodingOnce is the reason the type exists. The
// Scalar bundle is 3.7 MB on a route that skips authentication, so
// re-compressing per request turns a 100-byte GET into ~68ms of CPU that
// anyone can ask for. Counting passes rather than timing them keeps the test
// honest on a loaded machine.
func TestStaticBodyCompressesEachCodingOnce(t *testing.T) {
	data := []byte(strings.Repeat("compress me, but only once. ", 200))

	body := newStaticBody(data)

	// Re-memoize around a counter rather than wrapping the existing
	// OnceValue: wrapping counts calls, and every request makes one. What
	// has to be counted is compression passes.
	var passes int
	for _, coding := range compressibleEncodings {
		body.encoded[coding] = sync.OnceValue(func() []byte {
			passes++
			encoded, _ := encodeBody(data, coding)

			return encoded
		})
	}

	for range 5 {
		for _, encoding := range []string{"zstd", "gzip"} {
			req := httptest.NewRequest("GET", "/api/scalar.js", nil)
			req.Header.Set("Accept-Encoding", encoding)
			rec := httptest.NewRecorder()

			body.serve(rec, req)

			if got := rec.Header().Get("Content-Encoding"); got != encoding {
				t.Fatalf("Content-Encoding = %q, want %q", got, encoding)
			}
		}
	}

	if want := len(compressibleEncodings); passes != want {
		t.Errorf("compressed %d times over 10 requests, want %d — once per coding",
			passes, want)
	}
}

// TestStaticBodyServesIdentityUncompressed fails if a client that asks for no
// coding is handed a compressed body, or if the identity path allocates a
// compression pass it never uses.
func TestStaticBodyServesIdentityUncompressed(t *testing.T) {
	data := []byte(strings.Repeat("plain. ", 500))
	body := newStaticBody(data)

	rec := httptest.NewRecorder()
	body.serve(rec, httptest.NewRequest("GET", "/api", nil))

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want none", got)
	}
	if rec.Body.String() != string(data) {
		t.Error("identity response is not the original bytes")
	}
}

// TestStaticBodyRevalidatesWithoutCompressing pins that a 304 costs nothing:
// writeRawNegotiated returns before calling encode at all.
func TestStaticBodyRevalidatesWithoutCompressing(t *testing.T) {
	data := []byte(strings.Repeat("revalidate me. ", 200))
	body := newStaticBody(data)

	for _, coding := range compressibleEncodings {
		body.encoded[coding] = func() []byte {
			t.Error("a 304 compressed the body")

			return nil
		}
	}

	// Identity on both, so the validator names the same representation. A
	// tag obtained under one coding does not satisfy If-None-Match under
	// another — two codings are two representations, and only If-Match
	// resolves across them.
	first := httptest.NewRequest("GET", "/api", nil)
	firstRec := httptest.NewRecorder()
	body.serve(firstRec, first)

	second := httptest.NewRequest("GET", "/api", nil)
	second.Header.Set("If-None-Match", firstRec.Header().Get("ETag"))
	secondRec := httptest.NewRecorder()
	body.serve(secondRec, second)

	if secondRec.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", secondRec.Code)
	}
}
