package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/problem"
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

// negotiateCoding picks the content-coding for this response and stamps the
// headers, returning the coding rather than encoding anything -- so a caller
// can suffix its ETag and answer a 304 without a compression pass. Vary is
// added either way, with Add so it extends one another layer wrote.
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
// served under, per RFC 9110 §8.8.3: two codings are two representations and
// cannot share a strong validator. The suffix is appended to a hash of the
// identity bytes, so stripCodingSuffix can recover the underlying validator.
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
// time, whose ETag the caller hashed once at startup rather than per request.
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

	if refusedByPrecondition(w, r, etag, time.Time{}) {
		return
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
	writeCachedJSONStatusValidated(w, r, status, v, "")
}

// writeCachedJSONStatusValidated is writeCachedJSONStatus for a caller that
// already knows the validator: the one it offered on the conditional path, or
// a 304 and 200 that disagree make every revalidation miss. An empty validator
// means hash the body, for a representation not a pure function of the cache.
func writeCachedJSONStatusValidated(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	v any,
	validator string,
) {
	body, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		problem.Write(w, r, "API009", "failed to serialize response")
		return
	}

	if validator == "" {
		validator = computeETag(body)
	}

	writeRenderedJSON(w, r, status, renderedDoc{body: body, etag: validator}, time.Time{})
}

// writeRenderedJSON sends a JSON body whose validator is already known,
// negotiating a coding and answering a matching precondition with 304. A
// non-zero lastModified adds Last-Modified and evaluates If-Modified-Since.
// The write deadline lives here: the server sets no WriteTimeout of its own.
func writeRenderedJSON(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	doc renderedDoc,
	lastModified time.Time,
) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))

	coding := negotiateCoding(w, r, doc.body)
	etag := codedETag(doc.etag, coding)

	w.Header().Set("ETag", etag)
	setJSONContentType(w)
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache")
	}

	if !lastModified.IsZero() {
		lastModified = notAfterNow(lastModified)
		w.Header().Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	}

	if refusedByPrecondition(w, r, etag, lastModified) {
		return
	}

	if notModified(r, etag, lastModified) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	writeEncodedJSON(w, status, doc.body, coding)
}

// preconditionFailed evaluates the first steps of RFC 9110 §13.2.2, If-Match
// or else If-Unmodified-Since, and returns why the request must be refused, or
// "" when it may go on to If-None-Match. A zero lastModified is no date at all.
func preconditionFailed(r *http.Request, etag string, lastModified time.Time) string {
	if ifMatch := r.Header.Get("If-Match"); ifMatch != "" {
		if !etagMatchStrong(ifMatch, etag) {
			return "If-Match did not match the current state of the resource"
		}

		return ""
	}

	if modifiedAfter(r.Header.Get("If-Unmodified-Since"), lastModified) {
		return "the resource was modified after the If-Unmodified-Since date"
	}

	return ""
}

// modifiedAfter reports whether lastModified is later than the HTTP-date in
// field. A field that is not exactly one valid date never is.
func modifiedAfter(field string, lastModified time.Time) bool {
	if field == "" {
		return false
	}

	t, err := http.ParseTime(field)

	return err == nil &&
		notAfterNow(lastModified).Truncate(time.Second).After(t.Truncate(time.Second))
}

// notAfterNow is t, or now when t is later (RFC 9110 §8.8.2.1). The engine
// stamps these, and a manager whose clock runs ahead would otherwise hand a
// cache a date that never goes stale, and refuse the same date echoed back.
func notAfterNow(t time.Time) time.Time {
	if now := time.Now(); t.After(now) {
		return now
	}

	return t
}

// refusedByPrecondition answers 412 when preconditionFailed says so, and
// reports whether it did. The body is a problem, so no coding applies to it.
func refusedByPrecondition(
	w http.ResponseWriter,
	r *http.Request,
	etag string,
	lastModified time.Time,
) bool {
	reason := preconditionFailed(r, etag, lastModified)
	if reason == "" {
		return false
	}

	w.Header().Del("Content-Encoding")
	problem.Write(w, r, "API013", reason)

	return true
}

// notModified evaluates If-None-Match, or If-Modified-Since when that is absent
// and lastModified is known, as RFC 9110 §13.1.3 orders them.
func notModified(r *http.Request, etag string, lastModified time.Time) bool {
	inm := r.Header.Get("If-None-Match")
	if inm != "" || lastModified.IsZero() {
		return etagMatch(inm, etag)
	}

	ims := r.Header.Get("If-Modified-Since")
	if ims == "" {
		return false
	}

	t, err := http.ParseTime(ims)

	// HTTP dates have second precision; truncate for comparison.
	return err == nil && !lastModified.Truncate(time.Second).After(t.Truncate(time.Second))
}

// jsonTerminator ends every JSON body written by writeEncodedJSON.
var jsonTerminator = []byte("\n")

// writeEncodedJSON sends a JSON body and its trailing newline under coding.
// Compressed, the newline goes inside the frame, or a decoder finds a stray
// byte past its end. Uncompressed it is written separately: appending to
// json.Marshal's full-capacity buffer would copy the whole body.
func writeEncodedJSON(w http.ResponseWriter, status int, body []byte, coding Encoding) {
	if coding == EncodingIdentity {
		w.WriteHeader(status)
		w.Write(body)           //nolint:errcheck
		w.Write(jsonTerminator) //nolint:errcheck

		return
	}

	// Adding a byte cannot push a body back under the threshold, so this is
	// still the coding the ETag was suffixed with.
	payload, _ := encodeBody(append(body, '\n'), coding)

	w.WriteHeader(status)
	w.Write(payload) //nolint:errcheck
}

// writeCachedJSONTimed is like writeCachedJSON but also sets a Last-Modified
// header and evaluates If-Modified-Since, which If-None-Match overrides
// (RFC 9110 §13.1.3). Only for a body whose every input moves lastModified:
// one listing cross-references or filtered by grants would get a stale 304.
func writeCachedJSONTimed(w http.ResponseWriter, r *http.Request, v any, lastModified time.Time) {
	body, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		problem.Write(w, r, "API009", "failed to serialize response")
		return
	}

	w.Header().Set("Cache-Control", "no-cache")
	writeRenderedJSON(
		w,
		r,
		http.StatusOK,
		renderedDoc{body: body, etag: computeETag(body)},
		lastModified,
	)
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
