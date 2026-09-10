package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Encoding identifies a content-coding (RFC 9110 §8.4) that may be applied
// to a response body.
type Encoding int

const (
	EncodingIdentity Encoding = iota
	EncodingGzip
	EncodingZstd
)

// String returns the content-coding token as it appears in a
// Content-Encoding header.
func (e Encoding) String() string {
	switch e {
	case EncodingGzip:
		return "gzip"
	case EncodingZstd:
		return "zstd"
	case EncodingIdentity:
		return "identity"
	default:
		return "identity"
	}
}

// compressionThreshold is the minimum body size, in bytes, worth spending a
// compression pass on. Smaller bodies are served as identity regardless of
// what the client accepts.
const compressionThreshold = 1024

// zstdEncoder is a single package-level encoder shared by all requests.
// zstd.Encoder.EncodeAll is documented safe for concurrent use — each call
// checks out one of the encoder's internal workers for the duration of the
// call — so, unlike gzip.Writer, it needs no pool.
var zstdEncoder = mustZstdEncoder()

func mustZstdEncoder() *zstd.Encoder {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		panic("api: failed to create zstd encoder: " + err.Error())
	}
	return enc
}

// gzipWriterPool recycles gzip.Writers. Unlike the zstd encoder, gzip.Writer
// is not safe for concurrent use, so each caller must check one out.
var gzipWriterPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

// appliedEncoding reports the coding encodeBody will actually apply to a body
// of bodyLen bytes when asked for e — anything under compressionThreshold
// stays identity, however good the client's Accept-Encoding was.
//
// It is separate from encodeBody because a caller has to know the coding
// before it knows whether it needs a body at all: the coding goes into the
// ETag suffix and Content-Encoding, both of which a 304 carries even though
// it sends no bytes to compress.
func appliedEncoding(e Encoding, bodyLen int) Encoding {
	if bodyLen < compressionThreshold {
		return EncodingIdentity
	}

	return e
}

// encodeBody compresses body with the given coding, returning the encoded
// bytes and the coding actually applied. Callers can use the return value
// unconditionally: bodies under compressionThreshold, and requests for
// EncodingIdentity, come back as the original bytes with EncodingIdentity.
func encodeBody(body []byte, e Encoding) ([]byte, Encoding) {
	switch appliedEncoding(e, len(body)) {
	case EncodingGzip:
		return gzipEncode(body), EncodingGzip
	case EncodingZstd:
		return zstdEncoder.EncodeAll(body, nil), EncodingZstd
	default:
		return body, EncodingIdentity
	}
}

func gzipEncode(body []byte) []byte {
	var buf bytes.Buffer

	gw, _ := gzipWriterPool.Get().(*gzip.Writer)
	gw.Reset(&buf)
	gw.Write(body) //nolint:errcheck // writing into a bytes.Buffer cannot fail
	gw.Close()     //nolint:errcheck // flushing into a bytes.Buffer cannot fail
	gzipWriterPool.Put(gw)

	return buf.Bytes()
}

// resolveEncoding picks the content-coding to serve for r, per the
// Accept-Encoding negotiation rules of RFC 9110 §12.5.3.
//
// A coding's weight is its explicit q-value, falling back to the "*"
// entry's q-value when the coding isn't listed explicitly, falling back to
// 0 (unlisted, no wildcard) otherwise. A weight of 0 is refused. zstd and
// gzip are only ever chosen when their resolved weight is positive — an
// unlisted, non-wildcarded coding never outranks identity, which is why
// "gzip;q=0.5, zstd;q=0.9" prefers zstd (0.9) over an implicit identity
// weight rather than the reverse: identity is a fallback of last resort
// here, not a default q=1 competitor. Between two acceptable codings with
// equal weight, zstd wins (it compresses better); between zstd/gzip and
// identity at equal weight, the compressed coding wins.
//
// An absent Accept-Encoding header and an explicit "identity;q=0" both
// leave identity's own weight in play (0 for absent-and-unmentioned would
// still lose to any accepted zstd/gzip; 0 for an explicit refusal is
// identical) — the two cases are distinguished by whatever coding, if any,
// zstd/gzip end up resolving to, not by special-casing identity.
func resolveEncoding(r *http.Request) Encoding {
	header := r.Header.Get("Accept-Encoding")
	if header == "" {
		return EncodingIdentity
	}

	weights := parseAcceptEncoding(header)
	wildcard, hasWildcard := weights["*"]

	weightOf := func(coding string) float64 {
		if q, ok := weights[coding]; ok {
			return q
		}
		if hasWildcard {
			return wildcard
		}
		return 0
	}

	// gzip is considered before zstd so that, at equal weight, zstd's own
	// ">=" overrides it — which is what makes zstd the tie-break winner. Both
	// require a positive weight, so a coding the client never accepted cannot
	// be chosen just because identity's weight came back lower.
	best, bestQ := EncodingIdentity, weightOf("identity")

	if q := weightOf("gzip"); q > 0 && q >= bestQ {
		best, bestQ = EncodingGzip, q
	}
	if q := weightOf("zstd"); q > 0 && q >= bestQ {
		best = EncodingZstd
	}

	return best
}

// parseAcceptEncoding parses an Accept-Encoding field value into a map of
// lowercased coding token (including the literal "*") to its q-value,
// defaulting to 1 when a listed coding carries no q parameter.
func parseAcceptEncoding(header string) map[string]float64 {
	weights := make(map[string]float64)

	for entry := range strings.SplitSeq(header, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		coding, params, _ := strings.Cut(entry, ";")
		coding = strings.ToLower(strings.TrimSpace(coding))
		q := 1.0

		for param := range strings.SplitSeq(params, ";") {
			name, value, found := strings.Cut(param, "=")
			if !found || strings.ToLower(strings.TrimSpace(name)) != "q" {
				continue
			}
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				q = parsed
			}
		}

		weights[coding] = q
	}

	return weights
}

// compressionDisabledKey is the context key disableCompression/
// compressionDisabled use to mark a request as ineligible for response
// compression regardless of what it accepts.
type compressionDisabledKey struct{}

// disableCompression returns a copy of r whose context marks it as
// ineligible for response compression.
func disableCompression(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), compressionDisabledKey{}, true))
}

// compressionDisabled reports whether disableCompression was called on r.
func compressionDisabled(r *http.Request) bool {
	disabled, _ := r.Context().Value(compressionDisabledKey{}).(bool)
	return disabled
}
