package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The theme script has to run inline to beat the first paint, and the policy has
// no `script-src` to spare it, so the served document is hashed at startup
// rather than a digest pasted into the header — an edit to index.html must
// change the token the header carries, with nothing to remember to update.
func TestInlineScriptHashesCoverTheServedDocument(t *testing.T) {
	index := `<!doctype html><html><head>` +
		`<script>document.documentElement.classList.add("dark")</script>` +
		`<script type="module" src="/assets/index-abc.js"></script>` +
		`</head><body></body></html>`

	hashes, err := InlineScriptHashes(fstest.MapFS{
		"index.html": {Data: []byte(index)},
	})
	if err != nil {
		t.Fatalf("InlineScriptHashes: %v", err)
	}

	// One hash: the inline script. The `src` script already loads from 'self'.
	if len(hashes) != 1 {
		t.Fatalf("hashes=%v, want exactly one", hashes)
	}
	if !strings.HasPrefix(hashes[0], "'sha256-") || !strings.HasSuffix(hashes[0], "'") {
		t.Errorf("hash=%q, want a quoted 'sha256-…' token", hashes[0])
	}

	// Editing the script has to move the hash, or the header could go stale
	// against the document while still looking correct.
	edited, err := InlineScriptHashes(fstest.MapFS{
		"index.html": {Data: []byte(strings.Replace(index, `"dark"`, `"light"`, 1))},
	})
	if err != nil {
		t.Fatalf("InlineScriptHashes: %v", err)
	}
	if edited[0] == hashes[0] {
		t.Errorf("hash unchanged after editing the script: %q", hashes[0])
	}
}

func TestInlineScriptHashesReachTheHeader(t *testing.T) {
	hashes, err := InlineScriptHashes(fstest.MapFS{
		"index.html": {Data: []byte(`<head><script>var a=1</script></head>`)},
	})
	if err != nil {
		t.Fatalf("InlineScriptHashes: %v", err)
	}

	handler := securityHeaders(
		false,
		hashes,
	)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' "+hashes[0]) {
		t.Errorf("Content-Security-Policy=%q, want it to allow %s", csp, hashes[0])
	}
}

func TestInlineScriptHashesNeedsAnIndex(t *testing.T) {
	if _, err := InlineScriptHashes(fstest.MapFS{}); err == nil {
		t.Error("InlineScriptHashes on an FS with no index.html: want an error")
	}
}
