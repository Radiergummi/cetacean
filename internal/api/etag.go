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

// negotiateCoding picks the content-coding this response body will be served
// under and stamps the headers describing it. It returns the coding rather
// than encoding anything, so a caller can suffix its ETag and evaluate
// If-None-Match first: a 304 needs the validator but has no body to spend a
// compression pass on, and on a dashboard that polls, the 304 is the common
// path.
//
// Vary is added whether or not anything was compressed — the response varies
// by Accept-Encoding either way — and with Add rather than Set, so it extends
// the Vary another layer already wrote instead of replacing it.
func negotiateCoding(w http.ResponseWriter, r *http.Request, body []byte) Encoding {
	w.Header().Add("Vary", "Accept-Encoding")

	if compressionDisabled(r) {
		return EncodingIdentity
	}

	// Length first: parsing Accept-Encoding allocates, and almost every
	// client sends one, so a sub-threshold body would pay for a negotiation
	// whose result the threshold is about to discard. This *is* the threshold
	// gate — it is appliedEncoding's rule, reached before the negotiation
	// rather than after it — which is why what follows only has to ask what
	// the client accepts.
	if len(body) < compressionThreshold {
		return EncodingIdentity
	}

	coding := resolveEncoding(r)
	if coding == EncodingIdentity {
		return EncodingIdentity
	}

	w.Header().Set("Content-Encoding", coding.String())

	return coding
}

// codedETag marks a validator with the content-coding its representation was
// served under, per RFC 9110 §8.8.3: two codings of one resource are two
// representations and cannot share a strong validator.
//
// The suffix goes inside the quotes and is appended to a hash of the
// *identity* bytes, never of the encoded ones. That is what lets
// stripCodingSuffix recover the underlying validator, and so what lets a
// client hand back an If-Match it obtained under compression on a request
// precond evaluates without any — which, since every browser sends
// Accept-Encoding, is every conditional write a browser makes.
func codedETag(etag string, coding Encoding) string {
	if coding == EncodingIdentity {
		return etag
	}

	return `"` + strings.Trim(etag, `"`) + "-" + coding.String() + `"`
}

// writeRawWithETag sets an ETag on pre-rendered bytes and returns 304 Not
// Modified if the client's If-None-Match header matches. The caller must set
// Content-Type before calling this function. If the caller has already set
// Cache-Control, that value is kept; otherwise it defaults to "no-cache".
func writeRawWithETag(w http.ResponseWriter, r *http.Request, data []byte) {
	writeRawWithPrecomputedETag(w, r, data, computeETag(data))
}

// writeRawWithPrecomputedETag is writeRawWithETag for bodies fixed at build
// time, whose ETag the caller hashed once at startup rather than on every
// request — including the 304s, which never touch the body at all.
//
// The precomputed tag is the identity validator, so compression suffixes it
// and hashes nothing: there is no hash step here to redirect at the encoded
// bytes, and there must not be one.
func writeRawWithPrecomputedETag(
	w http.ResponseWriter,
	r *http.Request,
	data []byte,
	etag string,
) {
	coding := negotiateCoding(w, r, data)
	etag = codedETag(etag, coding)

	w.Header().Set("ETag", etag)

	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache")
	}

	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	body, _ := encodeBody(data, coding)

	w.WriteHeader(http.StatusOK)
	w.Write(body) //nolint:errcheck
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

	coding := negotiateCoding(w, r, body)
	etag := codedETag(computeETag(body), coding)

	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache")
	}

	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	writeEncodedJSON(w, status, body, coding)
}

// writeEncodedJSON sends a JSON body and its trailing newline under coding.
// The newline is compressed together with the body rather than written after
// it — a client decoding the frame would otherwise find a stray byte past its
// end.
func writeEncodedJSON(w http.ResponseWriter, status int, body []byte, coding Encoding) {
	// The coding is the one appliedEncoding already settled on, and adding a
	// byte cannot push a body back under the threshold, so the coding
	// encodeBody applies here is the coding the ETag was suffixed with.
	payload, _ := encodeBody(append(body, '\n'), coding)

	w.WriteHeader(status)
	w.Write(payload) //nolint:errcheck
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

	coding := negotiateCoding(w, r, body)
	etag := codedETag(computeETag(body), coding)

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

	writeEncodedJSON(w, http.StatusOK, body, coding)
}

// knownCodingSuffixes are the content-coding markers writeCachedJSON appends
// to an ETag. They are stripped before a precondition comparison: the suffix
// distinguishes representations for caching (RFC 9110 §8.8.3), but a
// precondition is an assertion about resource state, and a client's validator
// may have been obtained under different content negotiation than the request
// carrying it back.
var knownCodingSuffixes = []string{"-zstd", "-gzip"}

func stripCodingSuffix(opaqueTag string) string {
	for _, suffix := range knownCodingSuffixes {
		if trimmed, found := strings.CutSuffix(opaqueTag, suffix); found {
			return trimmed
		}
	}

	return opaqueTag
}

// etagMatchStrong reports whether an If-Match header matches etag using strong
// comparison, per RFC 9110 §13.1.1. Unlike etagMatch — which serves
// If-None-Match and therefore compares weakly per §13.1.2 — a weak validator
// never matches here.
func etagMatchStrong(header, etag string) bool {
	if header == "" {
		return false
	}
	if strings.TrimSpace(header) == "*" {
		return true
	}

	want := stripCodingSuffix(strings.Trim(etag, `"`))

	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(candidate, "W/") {
			continue
		}
		if stripCodingSuffix(strings.Trim(candidate, `"`)) == want {
			return true
		}
	}

	return false
}
