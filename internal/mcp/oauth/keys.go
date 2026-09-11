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

// Changing any of these changes every derived key: live tokens stop verifying
// and consent forms in flight are refused.
const (
	hkdfSalt    = "cetacean/mcp/hkdf/v1"
	csrfKeyInfo = "cetacean/mcp/csrf/v1"
	signKeyInfo = "cetacean/mcp/jwt/es256/v1"
)

const derivedKeyBytes = 32

// A derived scalar falls outside [1, n-1] with probability about 2^-32.
const maxScalarAttempts = 8

type keyMaterial struct {
	csrf   []byte
	signer *ecdsa.PrivateKey
	kid    string
}

// Extract runs as well as Expand: a configured root is not uniform key
// material.
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

// The counter in the info string keeps a retry deterministic, so the key stays
// a function of the root alone.
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

// A kid derived from the key itself needs neither storage nor configuration.
func thumbprint(pub *ecdsa.PublicKey) (string, error) {
	sum, err := (&jose.JSONWebKey{Key: pub}).Thumbprint(crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("oauth: JWK thumbprint: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(sum), nil
}
