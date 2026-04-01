package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

// computeETag returns a quoted ETag string from the SHA-256 of body,
// truncated to 16 bytes (32 hex characters).
func computeETag(body []byte) string {
	h := sha256.Sum256(body)
	return `"` + hex.EncodeToString(h[:16]) + `"`
}

// etagMatch reports whether the If-None-Match header matches the given strong ETag.
// Handles multiple comma-separated ETags, weak ETags (W/"..."), and the wildcard "*".
// Uses weak comparison per RFC 9110 Section 13.1.2 (appropriate for GET/HEAD).
func etagMatch(header, etag string) bool {
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	// Extract the opaque-tag from our strong ETag (strip quotes).
	opaqueTag := strings.TrimPrefix(etag, `"`)
	opaqueTag = strings.TrimSuffix(opaqueTag, `"`)

	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		// Strip weak prefix if present.
		candidate = strings.TrimPrefix(candidate, "W/")
		// Strip quotes.
		candidate = strings.TrimPrefix(candidate, `"`)
		candidate = strings.TrimSuffix(candidate, `"`)
		if candidate == opaqueTag {
			return true
		}
	}
	return false
}

// writeCachedJSON marshals v to JSON with ETag-based conditional caching.
// Returns 304 Not Modified if the client's If-None-Match header matches.
func writeCachedJSON(w http.ResponseWriter, r *http.Request, v any) {
	writeCachedJSONStatus(w, r, http.StatusOK, v)
}

// writeCachedJSONStatus is like writeCachedJSON but uses a custom status code.
// If the client's If-None-Match header matches the ETag, 304 is returned
// regardless of the requested status code (per RFC 9110 §13.1.2).
func writeCachedJSONStatus(w http.ResponseWriter, r *http.Request, status int, v any) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))

	body, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeErrorCode(w, r, "API009", "failed to serialize response")
		return
	}

	etag := computeETag(body)
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.WriteHeader(status)
	w.Write(body)         //nolint:errcheck
	w.Write([]byte{'\n'}) //nolint:errcheck
}

// writeCachedJSONTimed is like writeCachedJSON but also sets a Last-Modified
// header and evaluates If-Modified-Since for conditional requests. Per
// RFC 9110 §13.1.3, If-None-Match takes precedence over If-Modified-Since
// when both are present.
func writeCachedJSONTimed(w http.ResponseWriter, r *http.Request, v any, lastModified time.Time) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))

	body, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		writeErrorCode(w, r, "API009", "failed to serialize response")
		return
	}

	etag := computeETag(body)
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	if !lastModified.IsZero() {
		w.Header().Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	}

	// RFC 9110 §13.1.3: If-None-Match takes precedence over If-Modified-Since.
	if r.Header.Get("If-None-Match") != "" {
		if etagMatch(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	} else if !lastModified.IsZero() {
		if ims := r.Header.Get("If-Modified-Since"); ims != "" {
			if t, err := http.ParseTime(ims); err == nil {
				// HTTP dates have second precision; truncate for comparison.
				if !lastModified.Truncate(time.Second).After(t.Truncate(time.Second)) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}
	}

	w.Write(body)         //nolint:errcheck
	w.Write([]byte{'\n'}) //nolint:errcheck
}
