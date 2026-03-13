package auth

import (
	"net/http"
	"net/http/httptest"
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
