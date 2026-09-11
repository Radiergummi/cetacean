package oauth

import (
	"bytes"
	"crypto/elliptic"
	"encoding/base64"
	"errors"
	"testing"
)

var testRoot = []byte("cetacean-test-root-32-bytes-ok!!")

// mustDeriveKeys derives from a root, failing the test if it cannot. The tests
// that exercise derivation failure call deriveKeys directly.
func mustDeriveKeys(t *testing.T, root []byte) *keyMaterial {
	t.Helper()

	km, err := deriveKeys(root)
	if err != nil {
		t.Fatalf("deriveKeys: %v", err)
	}

	return km
}

func TestDeriveKeysIsDeterministic(t *testing.T) {
	first := mustDeriveKeys(t, testRoot)

	second := mustDeriveKeys(t, testRoot)

	if !bytes.Equal(first.csrf, second.csrf) {
		t.Error("CSRF key differs between derivations of the same root")
	}

	firstScalar, err := first.signer.Bytes()
	if err != nil {
		t.Fatalf("first.signer.Bytes: %v", err)
	}

	secondScalar, err := second.signer.Bytes()
	if err != nil {
		t.Fatalf("second.signer.Bytes: %v", err)
	}

	if !bytes.Equal(firstScalar, secondScalar) {
		t.Error("signing key differs between derivations of the same root")
	}

	if first.kid != second.kid {
		t.Errorf("kid differs between derivations: %q vs %q", first.kid, second.kid)
	}
}

func TestDeriveKeysSeparatesRootsAndPurposes(t *testing.T) {
	a := mustDeriveKeys(t, testRoot)

	b := mustDeriveKeys(t, []byte("a different root, 32 bytes long!"))

	if a.kid == b.kid {
		t.Error("two roots produced the same key")
	}

	// The two derived keys must not be each other, which is what a swapped or
	// shared info string would produce.
	signerScalar, err := a.signer.Bytes()
	if err != nil {
		t.Fatalf("a.signer.Bytes: %v", err)
	}

	if bytes.Equal(a.csrf, signerScalar) {
		t.Error("CSRF key and signing scalar are the same bytes")
	}

	if bytes.Equal(a.csrf, testRoot) {
		t.Error("CSRF key is the root itself")
	}
}

func TestDeriveSignerRetriesPastAnInvalidScalar(t *testing.T) {
	// A scalar of zero is not in [1, n-1]; ParseRawPrivateKey rejects it, and
	// derivation must move to the next counter rather than fail.
	valid := mustDeriveKeys(t, testRoot)

	var sawInfo []string

	key, err := deriveSigner(func(info string) ([]byte, error) {
		sawInfo = append(sawInfo, info)

		if len(sawInfo) == 1 {
			return make([]byte, 32), nil
		}

		return valid.signer.Bytes()
	})
	if err != nil {
		t.Fatalf("deriveSigner: %v", err)
	}

	keyScalar, err := key.Bytes()
	if err != nil {
		t.Fatalf("key.Bytes: %v", err)
	}

	validScalar, err := valid.signer.Bytes()
	if err != nil {
		t.Fatalf("valid.signer.Bytes: %v", err)
	}

	if !bytes.Equal(keyScalar, validScalar) {
		t.Error("retry did not return the second, valid scalar")
	}

	want := []string{
		"cetacean/mcp/jwt/es256/v1/0",
		"cetacean/mcp/jwt/es256/v1/1",
	}

	if len(sawInfo) != len(want) || sawInfo[0] != want[0] || sawInfo[1] != want[1] {
		t.Errorf("info strings = %q, want %q", sawInfo, want)
	}
}

func TestDeriveSignerGivesUp(t *testing.T) {
	_, err := deriveSigner(func(string) ([]byte, error) {
		return make([]byte, 32), nil
	})

	if err == nil {
		t.Fatal("want an error when no attempt yields a valid scalar")
	}
}

func TestDeriveKeysRefusesAnEmptyRoot(t *testing.T) {
	if _, err := deriveKeys(nil); !errors.Is(err, ErrMissingKey) {
		t.Errorf("error = %v, want ErrMissingKey", err)
	}
}

func TestDerivedKeyIsOnTheCurve(t *testing.T) {
	km := mustDeriveKeys(t, testRoot)

	if km.signer.Curve != elliptic.P256() {
		t.Errorf("curve = %v, want P-256", km.signer.Curve)
	}
}

// TestGoldenKID pins the thumbprint a fixed root produces. It is a drift
// detector for the JWK encoding go-jose does on our behalf; the independent
// check that the encoding is right lives in TestPublishedKeyVerifiesAToken.
func TestGoldenKID(t *testing.T) {
	km := mustDeriveKeys(t, testRoot)

	const want = "QAn0z6mB6vabOhSWFAGGkDsTDlNWpGM2lAV-uFBl6u8"

	if km.kid != want {
		t.Errorf("kid = %q, want %q", km.kid, want)
	}
}

// TestGoldenCSRFKey pins the CSRF key a fixed root produces, so csrfKeyInfo
// can drift without TestGoldenKID noticing. Produced by running deriveKeys
// against testRoot and asserting the output.
func TestGoldenCSRFKey(t *testing.T) {
	km := mustDeriveKeys(t, testRoot)

	const want = "dgol-oeS1eCZrySpXv5JNXv-JWv8cSPkTHHxwCIZR-E"

	if got := base64.RawURLEncoding.EncodeToString(km.csrf); got != want {
		t.Errorf("csrf = %q, want %q", got, want)
	}
}
