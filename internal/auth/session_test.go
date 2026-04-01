package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionCodecRoundTrip(t *testing.T) {
	codec := NewSessionCodec()

	id := &Identity{
		Subject:     "user-123",
		DisplayName: "Alice",
		Email:       "alice@example.com",
		Groups:      []string{"admin", "dev"},
		Provider:    "oidc",
		Raw:         map[string]any{"custom": "value"},
	}

	w := httptest.NewRecorder()
	codec.Set(w, id, 3600*time.Second)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != cookieName {
		t.Fatalf("expected cookie name %q, got %q", cookieName, cookie.Name)
	}
	if !cookie.HttpOnly {
		t.Fatal("expected HttpOnly")
	}
	if !cookie.Secure {
		t.Fatal("expected Secure")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("expected SameSite=Lax")
	}
	if cookie.Path != "/" {
		t.Fatalf("expected Path=/,  got %q", cookie.Path)
	}
	if cookie.MaxAge != 3600 {
		t.Fatalf("expected MaxAge=3600, got %d", cookie.MaxAge)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)

	got, err := codec.Get(req)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	if got.Subject != id.Subject {
		t.Errorf("Subject: got %q, want %q", got.Subject, id.Subject)
	}
	if got.DisplayName != id.DisplayName {
		t.Errorf("DisplayName: got %q, want %q", got.DisplayName, id.DisplayName)
	}
	if got.Email != id.Email {
		t.Errorf("Email: got %q, want %q", got.Email, id.Email)
	}
	if got.Provider != id.Provider {
		t.Errorf("Provider: got %q, want %q", got.Provider, id.Provider)
	}
	if len(got.Groups) != len(id.Groups) {
		t.Errorf("Groups length: got %d, want %d", len(got.Groups), len(id.Groups))
	}
	for i := range id.Groups {
		if got.Groups[i] != id.Groups[i] {
			t.Errorf("Groups[%d]: got %q, want %q", i, got.Groups[i], id.Groups[i])
		}
	}

	// Raw claims should be excluded from the session cookie to avoid bloat.
	if got.Raw != nil {
		t.Errorf("Raw should be nil in session, got %v", got.Raw)
	}
}

func TestSessionCodecTamperedCookie(t *testing.T) {
	codec := NewSessionCodec()

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "garbage.data"})

	_, err := codec.Get(req)
	if err == nil {
		t.Fatal("expected error for tampered cookie")
	}
}

func TestSessionCodecDifferentKeys(t *testing.T) {
	codec1 := NewSessionCodec()
	codec2 := NewSessionCodec()

	id := &Identity{Subject: "user-1", Provider: "oidc"}

	w := httptest.NewRecorder()
	codec1.Set(w, id, time.Hour)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(w.Result().Cookies()[0])

	_, err := codec2.Get(req)
	if err == nil {
		t.Fatal("expected error when verifying with different key")
	}
}

func TestSessionCodecClear(t *testing.T) {
	codec := NewSessionCodec()

	w := httptest.NewRecorder()
	codec.Clear(w)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].MaxAge != -1 {
		t.Fatalf("expected MaxAge=-1, got %d", cookies[0].MaxAge)
	}
}

func TestSessionCodecExpired(t *testing.T) {
	codec := NewSessionCodec()
	id := &Identity{Subject: "user-1", Provider: "oidc"}

	// Set a session with a short TTL.
	w := httptest.NewRecorder()
	codec.Set(w, id, time.Hour)

	// Advance the clock past expiry.
	codec.now = func() time.Time { return time.Now().Add(2 * time.Hour) }

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(w.Result().Cookies()[0])

	_, err := codec.Get(req)
	if err == nil {
		t.Fatal("expected error for expired session")
	}
	if err.Error() != "auth: session expired" {
		t.Errorf("error = %q, want %q", err.Error(), "auth: session expired")
	}
}

func TestSessionCodecNotYetExpired(t *testing.T) {
	codec := NewSessionCodec()
	id := &Identity{Subject: "user-1", Provider: "oidc"}

	w := httptest.NewRecorder()
	codec.Set(w, id, time.Hour)

	// Advance clock to just before expiry.
	codec.now = func() time.Time { return time.Now().Add(59 * time.Minute) }

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(w.Result().Cookies()[0])

	got, err := codec.Get(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", got.Subject, "user-1")
	}
}

func TestNewSessionCodecWithKey(t *testing.T) {
	// Valid 32-byte hex key (64 hex chars).
	hexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	codec1, err := NewSessionCodecWithKey(hexKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Same key should produce a codec that can verify cookies from the first.
	codec2, err := NewSessionCodecWithKey(hexKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	id := &Identity{Subject: "user-1", Provider: "oidc"}
	w := httptest.NewRecorder()
	codec1.Set(w, id, time.Hour)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(w.Result().Cookies()[0])

	got, err := codec2.Get(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", got.Subject, "user-1")
	}
}

func TestNewSessionCodecWithKey_InvalidLength(t *testing.T) {
	_, err := NewSessionCodecWithKey("deadbeef")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestNewSessionCodecWithKey_InvalidHex(t *testing.T) {
	_, err := NewSessionCodecWithKey(
		"not-hex-at-all-not-hex-at-all-not-hex-at-all-not-hex-at-all-zzzz",
	)
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestSessionCodecConcurrentAccess(t *testing.T) {
	codec := NewSessionCodec()
	id := &Identity{Subject: "user-1", Provider: "oidc"}

	w := httptest.NewRecorder()
	codec.Set(w, id, time.Hour)
	cookie := w.Result().Cookies()[0]

	errs := make(chan error, 100)
	for range 100 {
		go func() {
			req := httptest.NewRequest("GET", "/", nil)
			req.AddCookie(cookie)
			_, err := codec.Get(req)
			errs <- err
		}()
	}
	for range 100 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Get failed: %v", err)
		}
	}
}

func TestSessionCodecCookieSizeLimit(t *testing.T) {
	codec := NewSessionCodec()

	// 50 groups with realistic names stays under 4KB. Enterprise deployments
	// with more groups should use shorter names or move to server-side sessions.
	groups := make([]string, 50)
	for i := range groups {
		groups[i] = fmt.Sprintf("group-%d", i)
	}

	id := &Identity{
		Subject:     "user-123",
		DisplayName: "Jane Doe",
		Email:       "jane.doe@corp.example.com",
		Groups:      groups,
		Provider:    "oidc",
		Raw:         map[string]any{"should": "be-stripped"},
	}

	w := httptest.NewRecorder()
	codec.Set(w, id, time.Hour)

	cookie := w.Result().Cookies()[0]
	if len(cookie.Value) > 4096 {
		t.Errorf("session cookie is %d bytes, exceeds 4KB browser limit", len(cookie.Value))
	}

	// Verify it round-trips correctly.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	got, err := codec.Get(req)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(got.Groups) != 50 {
		t.Errorf("Groups length = %d, want 50", len(got.Groups))
	}
	if got.Raw != nil {
		t.Error("Raw should be nil in session cookie")
	}
}

func TestSessionCodecDropsHintWhenCookieTooLarge(t *testing.T) {
	codec := NewSessionCodec()

	// Moderate groups + a large ID token hint should exceed the limit.
	groups := make([]string, 40)
	for i := range groups {
		groups[i] = fmt.Sprintf("enterprise-group-%d-with-long-name", i)
	}

	id := &Identity{
		Subject:     "user-123",
		DisplayName: "Jane Doe",
		Email:       "jane.doe@corp.example.com",
		Groups:      groups,
		Provider:    "oidc",
	}

	largeHint := strings.Repeat("x", 1500) // simulate a large JWT

	w := httptest.NewRecorder()
	codec.Set(w, id, time.Hour, largeHint)

	cookie := w.Result().Cookies()[0]
	if len(cookie.Value) > maxCookieValueLen {
		t.Errorf(
			"cookie value = %d bytes, want <= %d (hint should have been dropped)",
			len(cookie.Value),
			maxCookieValueLen,
		)
	}

	// The session should still be valid and readable.
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	env, err := codec.GetEnvelope(r)
	if err != nil {
		t.Fatalf("session unreadable after hint drop: %v", err)
	}
	if env.IDTokenHint != "" {
		t.Error("expected IDTokenHint to be empty after size-based drop")
	}
	if env.Identity.Subject != "user-123" {
		t.Errorf("Subject = %q, want %q", env.Identity.Subject, "user-123")
	}
}

func TestSessionCodecKeepsHintWhenCookieFits(t *testing.T) {
	codec := NewSessionCodec()

	id := &Identity{
		Subject:  "user-123",
		Provider: "oidc",
	}

	hint := "eyJhbGciOiJSUzI1NiJ9.small-token-payload.signature"

	w := httptest.NewRecorder()
	codec.Set(w, id, time.Hour, hint)

	cookie := w.Result().Cookies()[0]

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookie)
	env, err := codec.GetEnvelope(r)
	if err != nil {
		t.Fatal(err)
	}
	if env.IDTokenHint != hint {
		t.Errorf(
			"IDTokenHint = %q, want %q (should be preserved when cookie fits)",
			env.IDTokenHint,
			hint,
		)
	}
}
