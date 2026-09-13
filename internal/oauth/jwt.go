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
	"slices"
	"strings"
	"time"
)

// Callers distinguish these with errors.Is to build WWW-Authenticate responses.
var (
	ErrMalformedToken   = errors.New("malformed token")
	ErrInvalidSig       = errors.New("invalid signature")
	ErrTokenExpired     = errors.New("token expired")
	ErrIssuerMismatch   = errors.New("issuer mismatch")
	ErrAudienceMismatch = errors.New("audience mismatch")
	ErrMissingKey       = errors.New("signing key is empty")
	ErrIncompleteClaims = errors.New("claims incomplete")
)

// RFC 9068 §2.1 prefers the unprefixed spelling and §4 requires accepting both.
// The type is what lets a resource server refuse an ID token where an access
// token belongs.
const (
	accessTokenType     = "at+jwt"
	accessTokenTypeFull = "application/at+jwt"
)

// Verifying against a fixed-algorithm public key is what defeats alg
// substitution; checking the header too is belt and braces.
type jwtHeaderClaims struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// AccessTokenClaims holds the application-level claims carried by the JWT.
// Standard claims (iss, aud, exp, iat, jti) are managed internally.
//
// Email and DisplayName are here because the ACL resolves a user audience
// against subject or email: without them a grant matches one person's session
// and not their token. Raw claims stay out, as they do from the session cookie.
type AccessTokenClaims struct {
	Subject     string   `json:"sub"`
	Email       string   `json:"email,omitempty"`
	DisplayName string   `json:"name,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	ClientID    string   `json:"client_id,omitempty"`
}

// audience accepts both shapes RFC 7519 §4.1.3 allows — an array, or a bare
// string for a single audience — and emits the bare string, which is what this
// server mints. A validator that took only one shape would refuse a conformant
// token, and ours would then misread it as another issuer's.
type audience []string

func (a *audience) UnmarshalJSON(raw []byte) error {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		*a = audience{one}

		return nil
	}

	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return err
	}

	*a = many

	return nil
}

func (a audience) MarshalJSON() ([]byte, error) {
	if len(a) == 1 {
		return json.Marshal(a[0])
	}

	return json.Marshal([]string(a))
}

type jwtPayload struct {
	Issuer    string   `json:"iss"`
	Audience  audience `json:"aud"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	JTIID     string   `json:"jti"`

	// RFC 7519 §4.1.5: a token must not be accepted before nbf. Never minted
	// here, so omitempty keeps it off the wire, but it is honoured on the way in.
	NotBefore int64    `json:"nbf,omitempty"`
	Subject   string   `json:"sub"`
	Email     string   `json:"email,omitempty"`
	Name      string   `json:"name,omitempty"`
	Groups    []string `json:"groups,omitempty"`

	// No omitempty: RFC 9068 §2.2 requires the claim, and an empty one is
	// refused at mint time rather than elided here.
	ClientID string `json:"client_id"`
}

// The audience is per token, not per issuer: one server issues for several
// protected resources and a token must name the one it was bound to.
type TokenIssuer struct {
	signer *ecdsa.PrivateKey
	header string
	Issuer string
}

func NewTokenIssuer(root []byte, issuer string) (*TokenIssuer, error) {
	km, err := deriveKeys(root)
	if err != nil {
		return nil, err
	}

	return newTokenIssuer(km, issuer), nil
}

// For a caller that has already derived, so it does not derive twice.
func newTokenIssuer(km *keyMaterial, issuer string) *TokenIssuer {
	return &TokenIssuer{
		signer: km.signer,
		// kid is base64url of a hash, so it needs no JSON escaping.
		header: base64.RawURLEncoding.EncodeToString([]byte(
			`{"alg":"ES256","kid":"` + km.kid + `","typ":"` + accessTokenType + `"}`,
		)),
		Issuer: issuer,
	}
}

// RFC 7518 §3.4: R and S as fixed-width 32-byte integers, concatenated.
const es256SigBytes = 64

// Fixed width matters: a short R left unpadded verifies here and nowhere else.
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

// Anything but es256SigBytes raw bytes is refused, so a DER signature cannot
// be presented as one.
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

// audience is the identifier of the resource the grant was bound to, stamped
// as aud so a resource server can refuse a token minted for another one.
func (t *TokenIssuer) IssueAccessToken(
	claims AccessTokenClaims,
	aud string,
	ttl time.Duration,
) (string, error) {
	if t.signer == nil {
		return "", ErrMissingKey
	}

	// sub and client_id are the caller's; every other required claim is filled
	// in below and cannot arrive missing.
	if claims.Subject == "" {
		return "", fmt.Errorf("%w: sub is required (RFC 9068 §2.2)", ErrIncompleteClaims)
	}

	if claims.ClientID == "" {
		return "", fmt.Errorf("%w: client_id is required (RFC 9068 §2.2)", ErrIncompleteClaims)
	}

	// An unaudienced token would verify against every resource this server
	// serves, which is the confusion the audience exists to stop.
	if aud == "" {
		return "", fmt.Errorf("%w: aud is required (RFC 9068 §2.2)", ErrIncompleteClaims)
	}

	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		panic(fmt.Sprintf("jwt: crypto/rand.Read failed (host RNG broken): %v", err))
	}

	now := time.Now()
	payload := jwtPayload{
		Issuer:    t.Issuer,
		Audience:  audience{aud},
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
		JTIID:     base64.RawURLEncoding.EncodeToString(jtiBytes),
		Subject:   claims.Subject,
		Email:     claims.Email,
		Name:      claims.DisplayName,
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

// audience is the identifier of the resource doing the verifying. Comparison is
// exact: a token for a neighbouring resource, or for one whose path contains
// this one, is refused with ErrAudienceMismatch.
func (t *TokenIssuer) VerifyAccessToken(
	token, aud string,
) (*AccessTokenClaims, error) {
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
	// RFC 9068 §4: any other typ, an absent one included, is refused. Compared
	// without regard to case, because RFC 2045 makes a media type's type and
	// subtype case-insensitive and RFC 7515 §4.1.9 carries that into typ.
	if !strings.EqualFold(hdr.Typ, accessTokenType) &&
		!strings.EqualFold(hdr.Typ, accessTokenTypeFull) {
		return nil, fmt.Errorf("%w: unexpected typ %q", ErrMalformedToken, hdr.Typ)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode payload: %w", ErrMalformedToken, err)
	}

	var payload jwtPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("%w: JSON decode payload: %w", ErrMalformedToken, err)
	}

	// Before the signature, deliberately. iss says which issuer a token claims,
	// and a caller sharing the Authorization header with another one needs that
	// answer to route the token at all — checked after the signature, every
	// foreign token would report a bad signature instead, and be refused where it
	// should have been passed on. Reading an unverified claim to route on is safe
	// because it decides nothing else: a token that names us still has to survive
	// every check below.
	if payload.Issuer != t.Issuer {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrIssuerMismatch, payload.Issuer, t.Issuer)
	}

	if !verifyES256(&t.signer.PublicKey, parts[0]+"."+parts[1], parts[2]) {
		return nil, fmt.Errorf("%w: ES256 verification failed", ErrInvalidSig)
	}

	// Membership, still exact per element: a token may name several audiences and
	// reach any of them, but no identifier is ever treated as containing another.
	if !slices.Contains(payload.Audience, aud) {
		return nil, fmt.Errorf(
			"%w: got %q, want %q",
			ErrAudienceMismatch,
			[]string(payload.Audience),
			aud,
		)
	}

	if payload.NotBefore != 0 && time.Now().Before(time.Unix(payload.NotBefore, 0)) {
		return nil, fmt.Errorf(
			"%w: not valid before %v",
			ErrTokenExpired,
			time.Unix(payload.NotBefore, 0),
		)
	}

	// Enforced on the way in as well as at mint: an absent sub would reach the ACL
	// as the empty subject, which is nobody and matches nothing readable.
	if payload.Subject == "" || payload.ClientID == "" {
		return nil, fmt.Errorf("%w: sub and client_id are required (RFC 9068 §2.2)",
			ErrIncompleteClaims)
	}

	if time.Unix(payload.ExpiresAt, 0).Before(time.Now()) {
		return nil, fmt.Errorf(
			"%w: expired at %v",
			ErrTokenExpired,
			time.Unix(payload.ExpiresAt, 0),
		)
	}

	return &AccessTokenClaims{
		Subject:     payload.Subject,
		Email:       payload.Email,
		DisplayName: payload.Name,
		Groups:      payload.Groups,
		ClientID:    payload.ClientID,
	}, nil
}
