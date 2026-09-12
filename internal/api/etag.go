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
// under and stamps the headers describing it. It returns the coding rather than
// encoding anything, so a caller can suffix its ETag and answer a 304 without
// spending a compression pass on a body it will not send.
//
// Vary is added whether or not anything was compressed, with Add rather than
// Set so it extends a Vary another layer already wrote.
func negotiateCoding(w http.ResponseWriter, r *http.Request, body []byte) Encoding {
	w.Header().Add("Vary", "Accept-Encoding")

	if compressionDisabled(r) {
		return EncodingIdentity
	}

	// This is appliedEncoding's threshold gate, applied before the negotiation
	// rather than after it: parsing Accept-Encoding allocates, and the result
	// would only be discarded.
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
// The suffix is appended to a hash of the identity bytes, never of the encoded
// ones, so stripCodingSuffix can recover the underlying validator when a client
// hands back an If-Match obtained under a different negotiation.
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
// The precomputed tag is the identity validator: compression suffixes it and
// hashes nothing.
func writeRawWithPrecomputedETag(
	w http.ResponseWriter,
	r *http.Request,
	data []byte,
	etag string,
) {
	writeRawNegotiated(w, r, data, etag, func(coding Encoding) []byte {
		body, _ := encodeBody(data, coding)

		return body
	})
}

// writeRawNegotiated negotiates a coding, answers a matching precondition with
// 304 before touching the body, and otherwise writes what encode returns for
// the negotiated coding.
func writeRawNegotiated(
	w http.ResponseWriter,
	r *http.Request,
	identity []byte,
	etag string,
	encode func(Encoding) []byte,
) {
	coding := negotiateCoding(w, r, identity)
	etag = codedETag(etag, coding)

	w.Header().Set("ETag", etag)

	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache")
	}

	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(encode(coding)) //nolint:errcheck
}

// setJSONContentType labels the response as JSON unless the caller already
// named a more specific type built on it. A JSON Feed and a JSON-LD document
// are both JSON on the wire and both have their own media type; overwriting
// what the handler set makes every route advertising one deny serving it.
func setJSONContentType(w http.ResponseWriter) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
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
	setJSONContentType(w)
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
// The newline is compressed together with the body: written after it, a client
// decoding the frame would find a stray byte past its end.
func writeEncodedJSON(w http.ResponseWriter, status int, body []byte, coding Encoding) {
	// Adding a byte cannot push a body back under the threshold, so this is
	// still the coding the ETag was suffixed with.
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
	setJSONContentType(w)
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

// knownCodingSuffixes are the content-coding markers codedETag appends. They
// are stripped before a precondition comparison: the suffix distinguishes
// representations for caching, but a precondition asserts resource state, and a
// validator may have been obtained under a different negotiation.
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
