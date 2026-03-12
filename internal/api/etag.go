package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	json "github.com/goccy/go-json"
)

// computeETag returns a quoted ETag string from the SHA-256 of body,
// truncated to 16 bytes (32 hex characters).
func computeETag(body []byte) string {
	h := sha256.Sum256(body)
	return `"` + hex.EncodeToString(h[:16]) + `"`
}

// writeJSONWithETag marshals v to JSON, computes an ETag, and returns
// 304 Not Modified if the client's If-None-Match header matches.
// ETag stability for map[string]any relies on goccy/go-json sorting map keys
// by default (unlike encoding/json). If switching JSON libraries, ensure map
// key ordering is deterministic or ETags will flap.
func writeJSONWithETag(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	etag := computeETag(body)
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json")

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Write(body)    //nolint:errcheck
	w.Write([]byte{'\n'}) //nolint:errcheck
}
