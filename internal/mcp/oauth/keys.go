package oauth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	jose "github.com/go-jose/go-jose/v4"
)

// The salt and the two info strings are the wire contract: change any of them
// and every derived key changes, so every live access token stops verifying
// and every consent form in flight is refused.
const (
	hkdfSalt    = "cetacean/mcp/hkdf/v1"
	csrfKeyInfo = "cetacean/mcp/csrf/v1"
	signKeyInfo = "cetacean/mcp/jwt/es256/v1"
)

// derivedKeyBytes is both the HMAC key length and the P-256 scalar length.
const derivedKeyBytes = 32

// maxScalarAttempts bounds the retry in deriveSigner. A scalar derived from
// uniform bytes falls outside [1, n-1] with probability about 2^-32.
const maxScalarAttempts = 8

// keyMaterial is everything the OAuth server derives from its root secret.
type keyMaterial struct {
	csrf   []byte
	signer *ecdsa.PrivateKey
	kid    string
}

// deriveKeys expands the root secret into the consent CSRF key and the ES256
// signing key. Extract runs as well as Expand: the root is whatever an
// operator configured, which RFC 5869 §3.3 does not let us assume is uniform.
func deriveKeys(root []byte) (*keyMaterial, error) {
	if len(root) == 0 {
		return nil, ErrMissingKey
	}

	csrf, err := hkdf.Key(sha256.New, root, []byte(hkdfSalt), csrfKeyInfo, derivedKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("oauth: derive CSRF key: %w", err)
	}

	signer, err := deriveSigner(func(info string) ([]byte, error) {
		return hkdf.Key(sha256.New, root, []byte(hkdfSalt), info, derivedKeyBytes)
	})
	if err != nil {
		return nil, err
	}

	kid, err := thumbprint(&signer.PublicKey)
	if err != nil {
		return nil, err
	}

	return &keyMaterial{csrf: csrf, signer: signer, kid: kid}, nil
}

// deriveSigner turns derived bytes into a P-256 key, retrying with an
// incremented counter in the info string when the scalar is out of range, so
// the outcome stays a function of the root alone.
func deriveSigner(expand func(info string) ([]byte, error)) (*ecdsa.PrivateKey, error) {
	for attempt := range maxScalarAttempts {
		scalar, err := expand(fmt.Sprintf("%s/%d", signKeyInfo, attempt))
		if err != nil {
			return nil, fmt.Errorf("oauth: derive signing key: %w", err)
		}

		key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
		if err == nil {
			return key, nil
		}
	}

	return nil, errors.New("oauth: no valid P-256 scalar within the attempt bound")
}

// thumbprint is the RFC 7638 JWK thumbprint, which is what a kid is here: a
// function of the key, so it needs neither storage nor configuration.
func thumbprint(pub *ecdsa.PublicKey) (string, error) {
	sum, err := (&jose.JSONWebKey{Key: pub}).Thumbprint(crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("oauth: JWK thumbprint: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(sum), nil
}
