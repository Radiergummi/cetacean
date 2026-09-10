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
			case EncodingIdentity:
				t.Fatal("unreachable: not exercised by this table")
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
