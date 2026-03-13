package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

const cookieName = "cetacean_session"

// SessionCodec signs and verifies session cookies using HMAC-SHA256.
type SessionCodec struct {
	key []byte
}

// NewSessionCodec creates a SessionCodec with a 32-byte random key.
// Panics if key generation fails.
func NewSessionCodec() *SessionCodec {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("auth: failed to generate session key: " + err.Error())
	}
	return &SessionCodec{key: key}
}

// Set serializes the identity, signs it, and sets it as a cookie.
func (s *SessionCodec) Set(w http.ResponseWriter, id *Identity, ttl time.Duration) {
	payload, err := json.Marshal(id)
	if err != nil {
		panic("auth: failed to marshal identity: " + err.Error())
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

	var id Identity
	if err := json.Unmarshal(payload, &id); err != nil {
		return nil, errors.New("auth: invalid session payload")
	}
	return &id, nil
}

// Clear deletes the session cookie.
func (s *SessionCodec) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
