package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)

type mockWhoIsClient struct {
	result *apitype.WhoIsResponse
	err    error
}

func (m *mockWhoIsClient) WhoIs(_ context.Context, _ string) (*apitype.WhoIsResponse, error) {
	return m.result, m.err
}

func TestTailscaleProvider_Authenticate_Success(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{
					ID:          42,
					LoginName:   "alice@example.com",
					DisplayName: "Alice Smith",
				},
				Node: &tailcfg.Node{
					Name: "alice-laptop.tail1234.ts.net.",
				},
			},
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "100.64.0.1:12345"

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id.Subject != "42" {
		t.Errorf("Subject = %q, want %q", id.Subject, "42")
	}
	if id.DisplayName != "Alice Smith" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "Alice Smith")
	}
	if id.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", id.Email, "alice@example.com")
	}
	if id.Provider != "tailscale" {
		t.Errorf("Provider = %q, want %q", id.Provider, "tailscale")
	}
	if id.Raw["node_name"] != "alice-laptop.tail1234.ts.net." {
		t.Errorf("Raw[node_name] = %v, want %q", id.Raw["node_name"], "alice-laptop.tail1234.ts.net.")
	}
	if id.Raw["user_id"] != int64(42) {
		t.Errorf("Raw[user_id] = %v, want %d", id.Raw["user_id"], 42)
	}
	if id.Raw["login_name"] != "alice@example.com" {
		t.Errorf("Raw[login_name] = %v, want %q", id.Raw["login_name"], "alice@example.com")
	}
}

func TestTailscaleProvider_Authenticate_Error(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			err: errors.New("not a tailscale address"),
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.1:12345"

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if id != nil {
		t.Fatalf("expected nil identity, got %+v", id)
	}
}

func TestTailscaleProvider_RegisterRoutes(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{},
	}

	mux := http.NewServeMux()
	p.RegisterRoutes(mux) // must not panic
}
