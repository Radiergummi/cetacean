package oauth

import (
	"bytes"
	"crypto/elliptic"
	"errors"
	"testing"
)

var testRoot = []byte("cetacean-test-root-32-bytes-ok!!")

func TestDeriveKeysIsDeterministic(t *testing.T) {
	first, err := deriveKeys(testRoot)
	if err != nil {
		t.Fatalf("deriveKeys: %v", err)
	}

	second, err := deriveKeys(testRoot)
	if err != nil {
		t.Fatalf("deriveKeys: %v", err)
	}

	if !bytes.Equal(first.csrf, second.csrf) {
		t.Error("CSRF key differs between derivations of the same root")
	}

	if first.signer.D.Cmp(second.signer.D) != 0 {
		t.Error("signing key differs between derivations of the same root")
	}

	if first.kid != second.kid {
		t.Errorf("kid differs between derivations: %q vs %q", first.kid, second.kid)
	}
}

func TestDeriveKeysSeparatesRootsAndPurposes(t *testing.T) {
	a, err := deriveKeys(testRoot)
	if err != nil {
		t.Fatalf("deriveKeys: %v", err)
	}

	b, err := deriveKeys([]byte("a different root, 32 bytes long!"))
	if err != nil {
		t.Fatalf("deriveKeys: %v", err)
	}

	if a.kid == b.kid {
		t.Error("two roots produced the same key")
	}

	// The two derived keys must not be each other, which is what a swapped or
	// shared info string would produce.
	if bytes.Equal(a.csrf, a.signer.D.Bytes()) {
		t.Error("CSRF key and signing scalar are the same bytes")
	}

	if bytes.Equal(a.csrf, testRoot) {
		t.Error("CSRF key is the root itself")
	}
}

func TestDeriveSignerRetriesPastAnInvalidScalar(t *testing.T) {
	// A scalar of zero is not in [1, n-1]; ParseRawPrivateKey rejects it, and
	// derivation must move to the next counter rather than fail.
	valid, err := deriveKeys(testRoot)
	if err != nil {
		t.Fatalf("deriveKeys: %v", err)
	}

	var sawInfo []string

	key, err := deriveSigner(func(info string) ([]byte, error) {
		sawInfo = append(sawInfo, info)

		if len(sawInfo) == 1 {
			return make([]byte, 32), nil
		}

		return valid.signer.D.FillBytes(make([]byte, 32)), nil
	})
	if err != nil {
		t.Fatalf("deriveSigner: %v", err)
	}

	if key.D.Cmp(valid.signer.D) != 0 {
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
	km, err := deriveKeys(testRoot)
	if err != nil {
		t.Fatalf("deriveKeys: %v", err)
	}

	if km.signer.Curve != elliptic.P256() {
		t.Errorf("curve = %v, want P-256", km.signer.Curve)
	}
}

// TestGoldenKID pins the thumbprint a fixed root produces. It is a drift
// detector for the JWK encoding go-jose does on our behalf; the independent
// check that the encoding is right lives in TestPublishedKeyVerifiesAToken.
func TestGoldenKID(t *testing.T) {
	km, err := deriveKeys(testRoot)
	if err != nil {
		t.Fatalf("deriveKeys: %v", err)
	}

	const want = "QAn0z6mB6vabOhSWFAGGkDsTDlNWpGM2lAV-uFBl6u8"

	if km.kid != want {
		t.Errorf("kid = %q, want %q", km.kid, want)
	}
}
