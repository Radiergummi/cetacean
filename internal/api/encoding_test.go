package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestResolveEncoding(t *testing.T) {
	cases := []struct {
		header string
		want   Encoding
	}{
		{"", EncodingIdentity},
		{"gzip", EncodingGzip},
		{"zstd", EncodingZstd},
		{"gzip, zstd", EncodingZstd},     // zstd preferred
		{"zstd;q=0, gzip", EncodingGzip}, // zstd explicitly refused
		{"gzip;q=0.5, zstd;q=0.9", EncodingZstd},
		{"gzip;q=0.9, zstd;q=0.5", EncodingGzip},
		{"br", EncodingIdentity}, // unsupported
		{"*", EncodingZstd},
		{"identity", EncodingIdentity},
		// A compressed coding wins a tie against identity, and zstd wins a
		// tie against gzip.
		{"gzip, identity", EncodingGzip},
		{"gzip;q=1.0, identity;q=1.0", EncodingGzip},
		{"gzip, zstd, identity", EncodingZstd},
		// A coding the client never listed is never chosen, however low the
		// weight it is competing against.
		{"identity;q=-1", EncodingIdentity},
		// A q-value outside 0-1 is malformed, not an emphatic preference.
		// Clamped to 1, gzip merely ties with zstd, and zstd takes the tie;
		// taken at face value, 5 would have won it.
		{"gzip;q=5, zstd", EncodingZstd},
	}

	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/nodes", nil)
			if tc.header != "" {
				r.Header.Set("Accept-Encoding", tc.header)
			}
			if got := resolveEncoding(r); got != tc.want {
				t.Errorf("resolveEncoding(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

func TestEncodeBodyRoundTrips(t *testing.T) {
	body := bytes.Repeat([]byte(`{"name":"service"},`), 200)

	for _, e := range []Encoding{EncodingGzip, EncodingZstd} {
		t.Run(e.String(), func(t *testing.T) {
			encoded, applied := encodeBody(body, e)
			if applied != e {
				t.Fatalf("applied = %v, want %v", applied, e)
			}
			if len(encoded) >= len(body) {
				t.Errorf("encoded %d bytes >= original %d", len(encoded), len(body))
			}

			var decoded []byte
			switch e {
			case EncodingGzip:
				gr, err := gzip.NewReader(bytes.NewReader(encoded))
				if err != nil {
					t.Fatalf("gzip.NewReader: %v", err)
				}
				decoded, err = io.ReadAll(gr)
				if err != nil {
					t.Fatalf("gzip read: %v", err)
				}
			case EncodingZstd:
				zr, err := zstd.NewReader(bytes.NewReader(encoded))
				if err != nil {
					t.Fatalf("zstd.NewReader: %v", err)
				}
				defer zr.Close()
				decoded, err = io.ReadAll(zr)
				if err != nil {
					t.Fatalf("zstd read: %v", err)
				}
			default:
				t.Fatalf("no decoder for %v", e)
			}

			if !bytes.Equal(decoded, body) {
				t.Error("decoded body does not match original")
			}
		})
	}
}

func TestEncodeBodySkipsSmallBodies(t *testing.T) {
	small := []byte(`{"ok":true}`)
	encoded, applied := encodeBody(small, EncodingZstd)

	if applied != EncodingIdentity {
		t.Errorf("applied = %v, want identity below the 1024-byte threshold", applied)
	}
	if !bytes.Equal(encoded, small) {
		t.Error("body was modified despite being under the threshold")
	}
}

func TestEncodeBodySkipsIdentity(t *testing.T) {
	body := bytes.Repeat([]byte("x"), compressionThreshold+1)
	encoded, applied := encodeBody(body, EncodingIdentity)

	if applied != EncodingIdentity {
		t.Errorf("applied = %v, want identity", applied)
	}
	if !bytes.Equal(encoded, body) {
		t.Error("body was modified for EncodingIdentity")
	}
}

func TestEncodingString(t *testing.T) {
	cases := []struct {
		e    Encoding
		want string
	}{
		{EncodingIdentity, "identity"},
		{EncodingGzip, "gzip"},
		{EncodingZstd, "zstd"},
	}

	for _, tc := range cases {
		if got := tc.e.String(); got != tc.want {
			t.Errorf("%v.String() = %q, want %q", tc.e, got, tc.want)
		}
	}
}

func TestCompressionDisabled(t *testing.T) {
	r := httptest.NewRequest("GET", "/nodes", nil)

	if compressionDisabled(r) {
		t.Fatal("compressionDisabled(r) = true before disableCompression was called")
	}

	r = disableCompression(r)

	if !compressionDisabled(r) {
		t.Error("compressionDisabled(r) = false after disableCompression was called")
	}
}

// Keeps compressibleEncodings from falling behind the Encoding enum: a coding
// added to the enum and not the list quietly narrows every test iterating it. It
// walks the enum by its encodingCount sentinel rather than by String(), so a
// member added to neither still fails here.
func TestCompressibleEncodingsCoversTheEnum(t *testing.T) {
	listed := make(map[Encoding]bool, len(compressibleEncodings))

	for _, coding := range compressibleEncodings {
		if coding == EncodingIdentity {
			t.Error("compressibleEncodings contains identity, which is the absence of a coding")
		}
		if listed[coding] {
			t.Errorf("compressibleEncodings lists %v twice", coding)
		}
		listed[coding] = true
	}

	for candidate := EncodingIdentity + 1; candidate < encodingCount; candidate++ {
		if !listed[candidate] {
			t.Errorf(
				"Encoding %d is in the enum but missing from compressibleEncodings",
				candidate,
			)
		}
		if candidate.String() == EncodingIdentity.String() {
			t.Errorf(
				"Encoding %d has no token of its own in String(); it would be sent "+
					"as Content-Encoding: identity",
				candidate,
			)
		}
	}
}
