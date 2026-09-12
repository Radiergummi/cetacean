package auth

import (
	"crypto/x509"
	"strings"
	"testing"
)

// FuzzDecodeClientCert exercises decodeClientCert, which decodes the RFC 9440
// Client-Cert header -- an RFC 8941 byte sequence carrying the DER certificate
// a trusted proxy forwarded. Its content authenticates the caller, and on a
// misconfigured deployment it is attacker-supplied. Two properties hold for
// any input: bytes are never returned alongside an error, and a successful
// decode yields something x509.ParseCertificate can rule on without panicking.
func FuzzDecodeClientCert(f *testing.F) {
	f.Add(":aGVsbG8gd29ybGQ=:") // well-formed colon-wrapped base64
	f.Add("aGVsbG8gd29ybGQ=")   // same payload, no colons
	f.Add("::")
	f.Add(":")
	f.Add("")
	f.Add(":not-base64!:")
	f.Add(":AAA:, :AAA:") // a duplicated header value joined into one string
	f.Add(":" + strings.Repeat("A", 1<<20) + ":")

	f.Fuzz(func(t *testing.T, value string) {
		der, err := decodeClientCert(value)
		if err != nil {
			if der != nil {
				t.Fatalf(
					"decodeClientCert(%q) returned %d bytes alongside an error: %v",
					value,
					len(der),
					err,
				)
			}
			return
		}

		// Must not panic, whatever it decides about the bytes.
		_, _ = x509.ParseCertificate(der)
	})
}
