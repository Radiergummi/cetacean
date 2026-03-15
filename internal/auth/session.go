package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

const cookieName = "cetacean_session"

// sessionEnvelope is the signed payload stored in the cookie. It wraps the
// identity with a server-side expiry timestamp that cannot be tampered with.
type sessionEnvelope struct {
	Identity  *Identity `json:"id"`
	ExpiresAt int64     `json:"exp"`
}

// SessionCodec signs and verifies session cookies using HMAC-SHA256.
type SessionCodec struct {
	key []byte
	now func() time.Time // for testing
}

// NewSessionCodec creates a SessionCodec with a random 32-byte key.
func NewSessionCodec() *SessionCodec {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("auth: failed to generate session key: " + err.Error())
	}
	return &SessionCodec{key: key, now: time.Now}
}

// NewSessionCodecWithKey creates a SessionCodec using the given hex-encoded key.
// The key must decode to exactly 32 bytes.
func NewSessionCodecWithKey(hexKey string) (*SessionCodec, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("auth: invalid session key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("auth: session key must be 32 bytes, got %d", len(key))
	}
	return &SessionCodec{key: key, now: time.Now}, nil
}

// Set serializes the identity with an expiry, signs it, and sets it as a cookie.
// Raw claims are excluded to keep the cookie compact (browsers enforce ~4KB).
func (s *SessionCodec) Set(w http.ResponseWriter, id *Identity, ttl time.Duration) {
	// Strip Raw claims to avoid cookie bloat from large IdP claim sets.
	compact := &Identity{
		Subject:     id.Subject,
		DisplayName: id.DisplayName,
		Email:       id.Email,
		Groups:      id.Groups,
		Provider:    id.Provider,
	}
	env := sessionEnvelope{
		Identity:  compact,
		ExpiresAt: s.now().Add(ttl).Unix(),
	}

	payload, err := json.Marshal(env)
	if err != nil {
		panic("auth: failed to marshal session: " + err.Error())
	}

	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	sig := mac.Sum(nil)

	value := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// Get reads and verifies the session cookie, returning the identity.
// Returns an error if the cookie is missing, tampered, or expired.
func (s *SessionCodec) Get(r *http.Request) (*Identity, error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("auth: malformed session cookie")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("auth: malformed session cookie payload")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("auth: malformed session cookie signature")
	}

	mac := hmac.New(sha256.New, s.key)
	mac.Write(payload)
	expected := mac.Sum(nil)

	if !hmac.Equal(sig, expected) {
		return nil, errors.New("auth: invalid session signature")
	}

	var env sessionEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, errors.New("auth: invalid session payload")
	}

	if s.now().Unix() >= env.ExpiresAt {
		return nil, errors.New("auth: session expired")
	}

	return env.Identity, nil
}

// Clear deletes the session cookie.
func (s *SessionCodec) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
