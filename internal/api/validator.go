package api

import (
	"encoding/binary"
	"hash/maphash"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
)

var validatorSeed = maphash.MakeSeed()

// derivedETag is a validator for a representation that is a pure function of
// the cache, the caller's grants and the request, computed without rendering.
// Every input that varies the body must be hashed here, or one caller gets a
// 304 for another's representation. codedETag adds the content coding.
func (h *Handlers) derivedETag(r *http.Request) string {
	var hash maphash.Hash
	hash.SetSeed(validatorSeed)

	var num [8]byte
	binary.LittleEndian.PutUint64(num[:], h.cache.Generation())
	_, _ = hash.Write(num[:])

	// Flapping lapses with time, with no cache mutation behind it.
	for _, id := range slices.Sorted(maps.Keys(h.cache.FlappingServices())) {
		_, _ = hash.WriteString(id)
		_, _ = hash.Write([]byte{0x1e})
	}

	// A service row carries what the image check last found, which changes
	// with no cache mutation behind it.
	if h.imageDigests != nil {
		binary.LittleEndian.PutUint64(num[:], h.imageDigests.Generation())
		_, _ = hash.Write(num[:])
	}

	identity := auth.IdentityFromContext(r.Context())
	binary.LittleEndian.PutUint64(num[:], h.acl.Fingerprint(identity))
	_, _ = hash.Write(num[:])

	// ContentType is an int enum; write it as bytes rather than rendering it,
	// so adding a media type cannot change an existing tag.
	binary.LittleEndian.PutUint64(num[:], uint64(ContentTypeFromContext(r.Context())))
	_, _ = hash.Write(num[:])

	// 0x1e between fields, so no concatenation of two of them can spell a
	// different pair of the same characters.
	for _, part := range []string{
		r.URL.Path,
		// Unnormalised: two spellings of one query miss rather than collide.
		r.URL.RawQuery,
		BasePathFromContext(r.Context()),
	} {
		_, _ = hash.WriteString(part)
		_, _ = hash.Write([]byte{0x1e})
	}

	return quoteETag(hash.Sum64())
}

// quoteETag renders a 64-bit validator as a quoted hex opaque-tag.
func quoteETag(sum uint64) string {
	const hex = "0123456789abcdef"

	out := make([]byte, 0, 18)
	out = append(out, '"')
	for shift := 60; shift >= 0; shift -= 4 {
		out = append(out, hex[(sum>>uint(shift))&0xf])
	}
	out = append(out, '"')

	return string(out)
}

// matchedValidator returns the coded tag the request's If-None-Match matches,
// or "". Before rendering the body length is unknown, so both identity and the
// negotiated coding are tried. A bare "*" waits for the writer: RFC 9110 §13.2.1
// ignores it unless the response would be 2xx, and the query is not yet parsed.
func matchedValidator(r *http.Request, validator string) string {
	inm := r.Header.Get("If-None-Match")
	if inm == "" || strings.TrimSpace(inm) == "*" {
		return ""
	}

	if tagged := codedETag(validator, EncodingIdentity); etagMatch(inm, tagged) {
		return tagged
	}

	if coding := resolveEncoding(r); coding != EncodingIdentity && !compressionDisabled(r) {
		if tagged := codedETag(validator, coding); etagMatch(inm, tagged) {
			return tagged
		}
	}

	return ""
}

// writeNotModified stamps the validator headers every 304 owes a client and
// answers it. Headers specific to a representation are the caller's to set
// first — a list still reports its Allow and Link-Template.
func writeNotModified(w http.ResponseWriter, tagged string) {
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("ETag", tagged)

	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-cache")
	}

	w.WriteHeader(http.StatusNotModified)
}
