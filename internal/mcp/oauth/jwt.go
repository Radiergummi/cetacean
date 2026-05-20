package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel errors returned by VerifyAccessToken. Callers can use errors.Is to
// distinguish them when building WWW-Authenticate responses.
var (
	ErrMalformedToken   = errors.New("malformed token")
	ErrInvalidSig       = errors.New("invalid signature")
	ErrTokenExpired     = errors.New("token expired")
	ErrIssuerMismatch   = errors.New("issuer mismatch")
	ErrAudienceMismatch = errors.New("audience mismatch")
	ErrMissingKey       = errors.New("signing key is empty")
)

// jwtHeader is the base64url-encoded fixed header {"alg":"HS256","typ":"JWT"},
// precomputed once at package init.
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// AccessTokenClaims holds the application-level claims carried by the JWT.
// Standard claims (iss, aud, exp, iat, jti) are managed internally.
type AccessTokenClaims struct {
	Subject  string   `json:"sub"`
	Groups   []string `json:"groups,omitempty"`
	ClientID string   `json:"client_id,omitempty"`
}

// jwtPayload is the full JWT payload, including standard claims. Not exported.
type jwtPayload struct {
	Issuer    string   `json:"iss"`
	Audience  string   `json:"aud"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	JTIID     string   `json:"jti"`
	Subject   string   `json:"sub"`
	Groups    []string `json:"groups,omitempty"`
	ClientID  string   `json:"client_id,omitempty"`
}

// TokenIssuer issues and verifies HMAC-SHA256 JWTs.
type TokenIssuer struct {
	SigningKey []byte
	Issuer    string
	Audience  string
}

// IssueAccessToken mints a signed compact JWT for the given claims with the
// specified TTL. A unique 128-bit jti is generated for each token.
func (t *TokenIssuer) IssueAccessToken(claims AccessTokenClaims, ttl time.Duration) (string, error) {
	if len(t.SigningKey) == 0 {
		return "", ErrMissingKey
	}

	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		panic(fmt.Sprintf("jwt: crypto/rand.Read failed (host RNG broken): %v", err))
	}

	now := time.Now()
	payload := jwtPayload{
		Issuer:   t.Issuer,
		Audience: t.Audience,
		IssuedAt: now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
		JTIID:    base64.RawURLEncoding.EncodeToString(jtiBytes),
		Subject:  claims.Subject,
		Groups:   claims.Groups,
		ClientID: claims.ClientID,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("jwt: marshal payload: %w", err)
	}

	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := jwtHeader + "." + payloadEncoded
	sig := sign(t.SigningKey, signingInput)

	return signingInput + "." + sig, nil
}

// VerifyAccessToken parses and validates a compact JWT, returning the
// application claims on success. Returns a wrapped sentinel error on failure
// so callers can distinguish expiry from signature failures.
func (t *TokenIssuer) VerifyAccessToken(token string) (*AccessTokenClaims, error) {
	if len(t.SigningKey) == 0 {
		return nil, ErrMissingKey
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 segments, got %d", ErrMalformedToken, len(parts))
	}

	signingInput := parts[0] + "." + parts[1]
	expectedSig := sign(t.SigningKey, signingInput)

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, fmt.Errorf("%w: HMAC-SHA256 mismatch", ErrInvalidSig)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode payload: %v", ErrMalformedToken, err)
	}

	var payload jwtPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("%w: JSON decode payload: %v", ErrMalformedToken, err)
	}

	if payload.Issuer != t.Issuer {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrIssuerMismatch, payload.Issuer, t.Issuer)
	}

	if payload.Audience != t.Audience {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrAudienceMismatch, payload.Audience, t.Audience)
	}

	if time.Unix(payload.ExpiresAt, 0).Before(time.Now()) {
		return nil, fmt.Errorf("%w: expired at %v", ErrTokenExpired, time.Unix(payload.ExpiresAt, 0))
	}

	return &AccessTokenClaims{
		Subject:  payload.Subject,
		Groups:   payload.Groups,
		ClientID: payload.ClientID,
	}, nil
}

// sign computes the base64url-encoded HMAC-SHA256 of input using key.
func sign(key []byte, input string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
