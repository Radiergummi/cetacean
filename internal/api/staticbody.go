package api

import (
	"net/http"
	"sync"
)

// staticBody is a response body fixed for the life of the process, served
// with a validator hashed once and each content coding compressed once.
//
// writeRawWithPrecomputedETag compresses on every 200, which is the only
// option for a body that changes but pure waste for one that cannot. The
// Scalar bundle is 3.7 MB: gzipping it per request costs ~68ms of CPU and a
// megabyte of garbage, on a route that skips authentication and so can be
// asked for it by anyone. The attribution documents behind /-/licenses are
// the same shape at 1.3 MB.
//
// Codings are compressed lazily, so a binary nobody fetches the playground or
// the licence texts from pays nothing, and retained afterwards. Nothing is
// precomputed at startup: that would move the cost rather than remove it, and
// pay it for documents most deployments never request.
type staticBody struct {
	identity []byte
	etag     string

	// encoded holds one memoized compressor per coding, indexed by Encoding.
	// Identity is nil — it needs no compression pass — and served straight
	// from identity.
	encoded [encodingCount]func() []byte
}

// newStaticBody hashes data's validator and prepares a memoized compressor
// for each coding this server applies. data must not be modified afterwards.
func newStaticBody(data []byte) *staticBody {
	body := &staticBody{identity: data, etag: computeETag(data)}

	for _, coding := range compressibleEncodings {
		body.encoded[coding] = sync.OnceValue(func() []byte {
			encoded, _ := encodeBody(data, coding)

			return encoded
		})
	}

	return body
}

// serve writes the body under the negotiated coding, with the conditional
// handling every other JSON and raw response gets.
func (b *staticBody) serve(w http.ResponseWriter, r *http.Request) {
	writeRawNegotiated(w, r, b.identity, b.etag, b.encode)
}

// encode returns the body under one coding, compressing it on first use.
//
// negotiateCoding applies the size threshold before choosing a coding, so a
// body it names a coding for is always one worth compressing; a coding with
// no compressor is identity.
func (b *staticBody) encode(coding Encoding) []byte {
	if compress := b.encoded[coding]; compress != nil {
		return compress()
	}

	return b.identity
}
