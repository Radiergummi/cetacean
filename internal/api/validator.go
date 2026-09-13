package api

import (
	"encoding/binary"
	"hash/maphash"
	"net/http"

	"github.com/radiergummi/cetacean/internal/auth"
)

var validatorSeed = maphash.MakeSeed()

// derivedETag is a validator for a representation that is a pure function of
// the cache contents, the caller's grants and the request — computed without
// rendering the body, so a conditional GET can be answered before the work.
//
// Everything that varies the body has to be in here. A missing input does not
// merely lose a cache hit: it hands one caller a 304 for a representation that
// is not theirs. The inputs, and why each is one:
//
//   - the cache generation, which is what actually changes;
//   - the ACL fingerprint, because every list is filtered per identity and the
//     policy behind it is reloaded at runtime;
//   - the path and the raw query, which carry the route, the resource id and
//     every search, filter, sort and pagination parameter. The query goes in
//     unnormalised on purpose — two spellings of one query then miss rather
//     than risk colliding;
//   - the negotiated media type, since one resource is served as JSON, Atom,
//     CSV or JSON Feed;
//   - the Range header, which selects a slice of the collection;
//   - the base path, which absolute @id values are built from.
//
// The content coding is not here: codedETag suffixes it onto whatever this
// returns, the same as it does for a hashed body.
//
// TestDerivedETagVaries enumerates these and fails if a component stops
// mattering. Add an input to the response and it goes here first.
func (h *Handlers) derivedETag(r *http.Request) string {
	var hash maphash.Hash
	hash.SetSeed(validatorSeed)

	var num [8]byte
	binary.LittleEndian.PutUint64(num[:], h.cache.Generation())
	_, _ = hash.Write(num[:])

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
		r.URL.RawQuery,
		r.Header.Get("Range"),
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
