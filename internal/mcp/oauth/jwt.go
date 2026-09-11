package oauth

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
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
	ErrIncompleteClaims = errors.New("claims incomplete")
)

// accessTokenType is the JWT header type RFC 9068 §2.1 gives an OAuth 2.0
// access token, and accessTokenTypeFull the same media type spelled with the
// prefix §2.1 recommends omitting. We mint the short form and accept both on
// verification, because §4 requires a resource server to accept either.
//
// The type is what lets a resource server refuse an ID token where an access
// token belongs — the two are otherwise indistinguishable to anything but
// their claim set.
const (
	accessTokenType     = "at+jwt"
	accessTokenTypeFull = "application/at+jwt"
)

// jwtHeaderClaims is the minimal subset of a JWT header we inspect on verify.
// alg=none / alg=HS256 substitution attacks are blocked by verifying with a
// public key of a fixed algorithm (ES256): a token minted for any other alg
// cannot produce a signature that verifies against it.
type jwtHeaderClaims struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

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

	// ClientID carries no omitempty: RFC 9068 §2.2 requires the claim, so a
	// token without it is one no resource server may accept. IssueAccessToken
	// refuses to mint an empty one rather than leaving the tag to elide it.
	ClientID string `json:"client_id"`
}

// TokenIssuer issues and verifies ES256 JWTs. Build one with NewTokenIssuer,
// which derives the key from a root secret.
type TokenIssuer struct {
	signer   *ecdsa.PrivateKey
	header   string
	Issuer   string
	Audience string
}

// NewTokenIssuer derives the signing key from root and returns an issuer bound
// to it.
func NewTokenIssuer(root []byte, issuer, audience string) (*TokenIssuer, error) {
	km, err := deriveKeys(root)
	if err != nil {
		return nil, err
	}

	return newTokenIssuer(km, issuer, audience), nil
}

// newTokenIssuer builds an issuer from already-derived key material, so a
// caller that derives once does not derive again.
func newTokenIssuer(km *keyMaterial, issuer, audience string) *TokenIssuer {
	return &TokenIssuer{
		signer: km.signer,
		// kid is base64url of a hash, so it needs no JSON escaping.
		header: base64.RawURLEncoding.EncodeToString([]byte(
			`{"alg":"ES256","kid":"` + km.kid + `","typ":"` + accessTokenType + `"}`,
		)),
		Issuer:   issuer,
		Audience: audience,
	}
}

// es256SigBytes is the fixed length RFC 7518 §3.4 gives an ES256 signature:
// R and S as 32-byte big-endian integers, concatenated.
const es256SigBytes = 64

// signES256 signs the JWS signing input. R and S are written at fixed width —
// a short R left unpadded produces a signature this package would accept and
// every other implementation would reject.
func signES256(key *ecdsa.PrivateKey, input string) (string, error) {
	digest := sha256.Sum256([]byte(input))

	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", fmt.Errorf("jwt: sign: %w", err)
	}

	sig := make([]byte, es256SigBytes)
	r.FillBytes(sig[:es256SigBytes/2])
	s.FillBytes(sig[es256SigBytes/2:])

	return base64.RawURLEncoding.EncodeToString(sig), nil
}

// verifyES256 checks a signature that must be exactly es256SigBytes raw bytes,
// so an ASN.1-encoded one is refused rather than parsed.
func verifyES256(pub *ecdsa.PublicKey, input, sig string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || len(raw) != es256SigBytes {
		return false
	}

	digest := sha256.Sum256([]byte(input))

	return ecdsa.Verify(
		pub,
		digest[:],
		new(big.Int).SetBytes(raw[:es256SigBytes/2]),
		new(big.Int).SetBytes(raw[es256SigBytes/2:]),
	)
}

// IssueAccessToken mints a signed compact JWT for the given claims with the
// specified TTL. A unique 128-bit jti is generated for each token.
func (t *TokenIssuer) IssueAccessToken(
	claims AccessTokenClaims,
	ttl time.Duration,
) (string, error) {
	if t.signer == nil {
		return "", ErrMissingKey
	}

	// iss, aud, exp, iat and jti are the issuer's to fill in below. sub and
	// client_id are the caller's, so they are the two of RFC 9068 §2.2's
	// seven required claims that can arrive missing.
	if claims.Subject == "" {
		return "", fmt.Errorf("%w: sub is required (RFC 9068 §2.2)", ErrIncompleteClaims)
	}

	if claims.ClientID == "" {
		return "", fmt.Errorf("%w: client_id is required (RFC 9068 §2.2)", ErrIncompleteClaims)
	}

	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		panic(fmt.Sprintf("jwt: crypto/rand.Read failed (host RNG broken): %v", err))
	}

	now := time.Now()
	payload := jwtPayload{
		Issuer:    t.Issuer,
		Audience:  t.Audience,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
		JTIID:     base64.RawURLEncoding.EncodeToString(jtiBytes),
		Subject:   claims.Subject,
		Groups:    claims.Groups,
		ClientID:  claims.ClientID,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("jwt: marshal payload: %w", err)
	}

	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := t.header + "." + payloadEncoded

	sig, err := signES256(t.signer, signingInput)
	if err != nil {
		return "", err
	}

	return signingInput + "." + sig, nil
}

// VerifyAccessToken parses and validates a compact JWT, returning the
// application claims on success. Returns a wrapped sentinel error on failure
// so callers can distinguish expiry from signature failures.
func (t *TokenIssuer) VerifyAccessToken(token string) (*AccessTokenClaims, error) {
	if t.signer == nil {
		return nil, ErrMissingKey
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 segments, got %d", ErrMalformedToken, len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode header: %w", ErrMalformedToken, err)
	}
	var hdr jwtHeaderClaims
	if err := json.Unmarshal(headerJSON, &hdr); err != nil {
		return nil, fmt.Errorf("%w: JSON decode header: %w", ErrMalformedToken, err)
	}
	if hdr.Alg != "ES256" {
		return nil, fmt.Errorf("%w: unexpected alg %q", ErrMalformedToken, hdr.Alg)
	}
	// RFC 9068 §4: reject a token whose typ is anything but the access token
	// type. An absent typ is rejected with the rest — before this profile the
	// server minted "JWT" and waved absence through, and both now fail, which
	// costs a client holding one an extra refresh and nothing more: access
	// tokens are the only JWTs here, they are never persisted, and the
	// refresh token that replaces them is opaque.
	if hdr.Typ != accessTokenType && hdr.Typ != accessTokenTypeFull {
		return nil, fmt.Errorf("%w: unexpected typ %q", ErrMalformedToken, hdr.Typ)
	}

	if !verifyES256(&t.signer.PublicKey, parts[0]+"."+parts[1], parts[2]) {
		return nil, fmt.Errorf("%w: ES256 verification failed", ErrInvalidSig)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode payload: %w", ErrMalformedToken, err)
	}

	var payload jwtPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("%w: JSON decode payload: %w", ErrMalformedToken, err)
	}

	if payload.Issuer != t.Issuer {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrIssuerMismatch, payload.Issuer, t.Issuer)
	}

	if payload.Audience != t.Audience {
		return nil, fmt.Errorf(
			"%w: got %q, want %q",
			ErrAudienceMismatch,
			payload.Audience,
			t.Audience,
		)
	}

	if time.Unix(payload.ExpiresAt, 0).Before(time.Now()) {
		return nil, fmt.Errorf(
			"%w: expired at %v",
			ErrTokenExpired,
			time.Unix(payload.ExpiresAt, 0),
		)
	}

	return &AccessTokenClaims{
		Subject:  payload.Subject,
		Groups:   payload.Groups,
		ClientID: payload.ClientID,
	}, nil
}
