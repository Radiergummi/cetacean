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

// compressibleEncodings is every coding this server applies. knownCodingSuffixes
// is deliberately not derived from it: a list checked against itself cannot
// catch drift.
var compressibleEncodings = []Encoding{EncodingGzip, EncodingZstd}

// zstdEncoder is shared by all requests: zstd.Encoder.EncodeAll is documented
// safe for concurrent use, so unlike gzip.Writer it needs no pool.
var zstdEncoder = mustZstdEncoder()

func mustZstdEncoder() *zstd.Encoder {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		panic("api: failed to create zstd encoder: " + err.Error())
	}
	return enc
}

// gzipWriterPool recycles gzip.Writers, which are not safe for concurrent use.
var gzipWriterPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

// appliedEncoding reports the coding encodeBody will apply to a body of
// bodyLen bytes when asked for e; anything under compressionThreshold stays
// identity. It is separate from encodeBody because a 304 carries the coding on
// its ETag and Content-Encoding without having a body to compress.
func appliedEncoding(e Encoding, bodyLen int) Encoding {
	if bodyLen < compressionThreshold {
		return EncodingIdentity
	}

	return e
}

// encodeBody compresses body with the given coding, returning the encoded
// bytes and the coding actually applied. A body under compressionThreshold
// comes back unchanged as EncodingIdentity.
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

	// Reset off buf before pooling, or the writer pins this response's bytes
	// until it is checked out again.
	gw.Reset(io.Discard)
	gzipWriterPool.Put(gw)

	return buf.Bytes()
}

// resolveEncoding picks the content-coding to serve for r, per RFC 9110
// §12.5.3. A coding's weight is its q-value, else the "*" entry's, else 0;
// zstd and gzip need a positive weight to be chosen at all, so identity is a
// fallback of last resort rather than a q=1 competitor. At equal weight a
// compressed coding beats identity and zstd beats gzip.
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

	// gzip is considered first so zstd's ">=" overrides it at equal weight.
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
// lowercased coding token (including "*") to q-value, defaulting to 1 when a
// coding carries no q parameter. A weight outside RFC 9110 §12.4.2's 0–1 range
// is clamped into it: "gzip;q=5" is malformed, not a stronger preference.
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
				q = min(max(parsed, 0), 1)
			}
		}

		weights[coding] = q
	}

	return weights
}

// compressionDisabledKey marks a request as ineligible for compression.
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
