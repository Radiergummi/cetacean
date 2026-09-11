package api

import (
	"bytes"
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

// TestStaticBodyServesEachCodingUnderItsOwnLabel decodes every coding's
// response with the decoder its Content-Encoding named. encoded is an array
// indexed by the negotiated coding, so a wrong index serves one coding's frame
// under another's label — headers, status and pass count all stay correct
// while every gzip client receives a body gzip.NewReader rejects outright.
//
// It is separate from TestStaticBodyCompressesEachCodingOnce, which has to
// re-memoize encoded to count passes and therefore replaces the very wiring
// this asserts. Verified by having newStaticBody compress every coding as the
// last one: that test passes, this one fails.
func TestStaticBodyServesEachCodingUnderItsOwnLabel(t *testing.T) {
	data := []byte(strings.Repeat("label me correctly. ", 200))
	body := newStaticBody(data)

	for _, coding := range compressibleEncodings {
		t.Run(coding.String(), func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/scalar.js", nil)
			req.Header.Set("Accept-Encoding", coding.String())
			rec := httptest.NewRecorder()

			body.serve(rec, req)

			encoding := rec.Header().Get("Content-Encoding")
			if encoding != coding.String() {
				t.Fatalf("Content-Encoding = %q, want %q", encoding, coding)
			}

			if plain := decodeCoding(t, encoding, rec.Body.Bytes()); !bytes.Equal(plain, data) {
				t.Errorf("body labelled %q does not decode to the identity bytes", encoding)
			}
		})
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
//
// Both requests negotiate gzip, and the counter is a plain closure rather than
// a sync.OnceValue. Both details are load-bearing, and the first version of
// this test had neither, which left it unable to fail: under identity
// encoded[EncodingIdentity] is nil and serve never consults a compressor at
// all, so a stub that fails on call is unreachable whatever the code does; and
// memoizing would collapse a second call into the first, which is the very
// thing being counted. Verified by moving encode(coding) above the etagMatch
// check — the whole package passed before, this fails now.
func TestStaticBodyRevalidatesWithoutCompressing(t *testing.T) {
	data := []byte(strings.Repeat("revalidate me. ", 200))
	body := newStaticBody(data)

	var calls int
	body.encoded[EncodingGzip] = func() []byte {
		calls++
		encoded, _ := encodeBody(data, EncodingGzip)

		return encoded
	}

	// Same coding on both, so the validator names the same representation. A
	// tag obtained under one coding does not satisfy If-None-Match under
	// another — two codings are two representations, and only If-Match
	// resolves across them.
	first := httptest.NewRequest("GET", "/api", nil)
	first.Header.Set("Accept-Encoding", "gzip")
	firstRec := httptest.NewRecorder()
	body.serve(firstRec, first)

	if firstRec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", firstRec.Code)
	}
	if calls != 1 {
		t.Fatalf("the 200 compressed %d times, want 1", calls)
	}

	second := httptest.NewRequest("GET", "/api", nil)
	second.Header.Set("Accept-Encoding", "gzip")
	second.Header.Set("If-None-Match", firstRec.Header().Get("ETag"))
	secondRec := httptest.NewRecorder()
	body.serve(secondRec, second)

	if secondRec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", secondRec.Code)
	}
	if calls != 1 {
		t.Errorf("the 304 compressed the body: %d passes, want the 200's 1", calls)
	}
}
