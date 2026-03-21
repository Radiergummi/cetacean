package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/tailcfg"
)

type mockWhoIsClient struct {
	result  *apitype.WhoIsResponse
	err     error
	gotAddr string // records the remoteAddr passed to WhoIs
}

func (m *mockWhoIsClient) WhoIs(
	_ context.Context,
	remoteAddr string,
) (*apitype.WhoIsResponse, error) {
	m.gotAddr = remoteAddr
	return m.result, m.err
}

func TestTailscaleProvider_Authenticate_Success(t *testing.T) {
	mock := &mockWhoIsClient{
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
	}
	p := &TailscaleProvider{client: mock}

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
		t.Errorf(
			"Raw[node_name] = %v, want %q",
			id.Raw["node_name"],
			"alice-laptop.tail1234.ts.net.",
		)
	}
	if id.Raw["user_id"] != int64(42) {
		t.Errorf("Raw[user_id] = %v, want %d", id.Raw["user_id"], 42)
	}
	if id.Raw["login_name"] != "alice@example.com" {
		t.Errorf("Raw[login_name] = %v, want %q", id.Raw["login_name"], "alice@example.com")
	}
	if len(id.Groups) != 0 {
		t.Errorf("Groups = %v, want empty (no capability configured)", id.Groups)
	}

	// Verify RemoteAddr was forwarded to WhoIs.
	if mock.gotAddr != "100.64.0.1:12345" {
		t.Errorf("WhoIs called with %q, want %q", mock.gotAddr, "100.64.0.1:12345")
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

func TestTailscaleProvider_Authenticate_NonTailnetAddr(t *testing.T) {
	called := false
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{ID: 1, LoginName: "a@b.com", DisplayName: "A"},
				Node:        &tailcfg.Node{Name: "n"},
			},
		},
	}
	// Wrap to detect calls.
	p.client = &spyWhoIsClient{inner: p.client, called: &called}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.1:12345"

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error for non-tailnet address")
	}
	if id != nil {
		t.Fatal("expected nil identity")
	}
	if called {
		t.Error("WhoIs should not be called for non-tailnet addresses")
	}
}

func TestTailscaleProvider_Authenticate_NilUserProfile(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: nil,
				Node:        &tailcfg.Node{Name: "tagged-device.tail1234.ts.net."},
			},
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "100.64.0.1:12345"

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error for nil UserProfile")
	}
	if id != nil {
		t.Fatal("expected nil identity")
	}
	if !strings.Contains(err.Error(), "no user profile") {
		t.Errorf("error = %q, want mention of 'no user profile'", err.Error())
	}
}

func TestTailscaleProvider_Authenticate_NilNode(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{
					ID:          1,
					LoginName:   "user@example.com",
					DisplayName: "User",
				},
				Node: nil,
			},
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "100.64.0.1:12345"

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error for nil Node")
	}
	if id != nil {
		t.Fatal("expected nil identity")
	}
	if !strings.Contains(err.Error(), "no node info") {
		t.Errorf("error = %q, want mention of 'no node info'", err.Error())
	}
}

func TestValidateTailscaleAddr(t *testing.T) {
	tests := []struct {
		addr    string
		wantErr bool
	}{
		// Valid Tailscale CGNAT range (100.64.0.0/10).
		{"100.64.0.1:12345", false},
		{"100.100.100.100:443", false},
		{"100.127.255.254:1", false},

		// Valid Tailscale IPv6 ULA (fd7a:115c:a1e0::/48).
		{"[fd7a:115c:a1e0::1]:443", false},
		{"[fd7a:115c:a1e0:ab12::1]:8080", false},

		// Outside CGNAT range.
		{"192.168.1.1:12345", true},
		{"10.0.0.1:80", true},
		{"172.16.0.1:443", true},
		{"8.8.8.8:53", true},
		{"100.63.255.255:80", true}, // just below 100.64.0.0/10
		{"100.128.0.0:80", true},    // just above 100.64.0.0/10

		// IPv6 outside ULA.
		{"[::1]:80", true},
		{"[2001:db8::1]:443", true},
		{"[fd7a:115c:a1e1::1]:443", true}, // different /48

		// Malformed.
		{"not-an-address", true},
		{"", true},
	}

	for _, tt := range tests {
		err := validateTailscaleAddr(tt.addr)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateTailscaleAddr(%q) error = %v, wantErr = %v", tt.addr, err, tt.wantErr)
		}
	}
}

func TestTailscaleProvider_RegisterRoutes(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{},
	}

	mux := http.NewServeMux()
	p.RegisterRoutes(mux) // must not panic
}

func TestTailscaleProvider_Authenticate_IPv6ULA(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{
					ID:          7,
					LoginName:   "bob@example.com",
					DisplayName: "Bob",
				},
				Node: &tailcfg.Node{Name: "bob-desktop.tail1234.ts.net."},
			},
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[fd7a:115c:a1e0::1]:443"

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Subject != "7" {
		t.Errorf("Subject = %q, want %q", id.Subject, "7")
	}
}

func TestTailscaleProvider_Authenticate_EmptyProfile(t *testing.T) {
	// UserProfile exists but has empty strings — should still produce a
	// valid identity without panicking.
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{
					ID:          0,
					LoginName:   "",
					DisplayName: "",
				},
				Node: &tailcfg.Node{Name: ""},
			},
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "100.64.0.1:12345"

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Subject != "0" {
		t.Errorf("Subject = %q, want %q", id.Subject, "0")
	}
	if id.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty", id.DisplayName)
	}
	if id.Email != "" {
		t.Errorf("Email = %q, want empty", id.Email)
	}
}

func TestTailscaleProvider_Authenticate_CapabilityConfigured_NilCapMap(t *testing.T) {
	// Capability is configured but the WhoIs response has no CapMap at all.
	p := &TailscaleProvider{
		capability: "example.com/cap/cetacean",
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{
					ID: 1, LoginName: "a@b.com", DisplayName: "A",
				},
				Node:   &tailcfg.Node{Name: "n"},
				CapMap: nil,
			},
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "100.64.0.1:12345"

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(id.Groups) != 0 {
		t.Errorf("Groups = %v, want empty (nil CapMap)", id.Groups)
	}
}

func TestTailscaleProvider_Authenticate_CapabilityConfigured_NoMatch(t *testing.T) {
	// Capability is configured but the CapMap has a different key.
	p := &TailscaleProvider{
		capability: "example.com/cap/cetacean",
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{
					ID: 1, LoginName: "a@b.com", DisplayName: "A",
				},
				Node: &tailcfg.Node{Name: "n"},
				CapMap: tailcfg.PeerCapMap{
					"example.com/cap/other-app": {`{"groups":["admin"]}`},
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
	if len(id.Groups) != 0 {
		t.Errorf("Groups = %v, want empty (no matching capability)", id.Groups)
	}
}

// ---------------------------------------------------------------------------
// Whoami endpoint tests
// ---------------------------------------------------------------------------

func TestTailscaleProvider_Whoami_Success(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{
					ID: 42, LoginName: "alice@example.com", DisplayName: "Alice",
				},
				Node: &tailcfg.Node{Name: "n"},
			},
		},
	}

	handler := WhoamiHandler(p)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "100.64.0.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if !strings.Contains(w.Body.String(), `"subject":"42"`) {
		t.Errorf("body = %s, want subject 42", w.Body.String())
	}
}

func TestTailscaleProvider_Whoami_Unauthenticated(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			err: errors.New("not on tailnet"),
		},
	}

	handler := WhoamiHandler(p)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ---------------------------------------------------------------------------
// App capability (groups extraction) tests
// ---------------------------------------------------------------------------

func TestTailscaleProvider_Authenticate_WithCapability(t *testing.T) {
	p := &TailscaleProvider{
		capability: "example.com/cap/cetacean",
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
				CapMap: tailcfg.PeerCapMap{
					"example.com/cap/cetacean": {
						`{"groups":["admin","viewer"]}`,
					},
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
	if len(id.Groups) != 2 || id.Groups[0] != "admin" || id.Groups[1] != "viewer" {
		t.Errorf("Groups = %v, want [admin viewer]", id.Groups)
	}
}

func TestTailscaleProvider_Authenticate_NoCapabilityConfigured(t *testing.T) {
	p := &TailscaleProvider{
		// capability is empty — groups should not be extracted even if CapMap has data
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{
					ID:          42,
					LoginName:   "alice@example.com",
					DisplayName: "Alice",
				},
				Node: &tailcfg.Node{Name: "n"},
				CapMap: tailcfg.PeerCapMap{
					"example.com/cap/cetacean": {
						`{"groups":["admin"]}`,
					},
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
	if len(id.Groups) != 0 {
		t.Errorf("Groups = %v, want empty (no capability configured)", id.Groups)
	}
}

func TestExtractCapGroups(t *testing.T) {
	cap := tailcfg.PeerCapability("example.com/cap/cetacean")

	tests := []struct {
		name   string
		capMap tailcfg.PeerCapMap
		want   []string
	}{
		{
			name:   "single grant with groups",
			capMap: tailcfg.PeerCapMap{cap: {`{"groups":["admin","viewer"]}`}},
			want:   []string{"admin", "viewer"},
		},
		{
			name: "multiple grants merged",
			capMap: tailcfg.PeerCapMap{cap: {
				`{"groups":["admin"]}`,
				`{"groups":["viewer","ops"]}`,
			}},
			want: []string{"admin", "viewer", "ops"},
		},
		{
			name: "duplicate groups deduplicated",
			capMap: tailcfg.PeerCapMap{cap: {
				`{"groups":["admin","viewer"]}`,
				`{"groups":["viewer","admin"]}`,
			}},
			want: []string{"admin", "viewer"},
		},
		{
			name:   "no groups key in grant",
			capMap: tailcfg.PeerCapMap{cap: {`{"access":"rw"}`}},
			want:   nil,
		},
		{
			name:   "empty groups array",
			capMap: tailcfg.PeerCapMap{cap: {`{"groups":[]}`}},
			want:   nil,
		},
		{
			name:   "capability not in map",
			capMap: tailcfg.PeerCapMap{"other/cap": {`{"groups":["admin"]}`}},
			want:   nil,
		},
		{
			name:   "nil CapMap",
			capMap: nil,
			want:   nil,
		},
		{
			name:   "empty values for capability",
			capMap: tailcfg.PeerCapMap{cap: {}},
			want:   nil,
		},
		{
			name:   "malformed JSON skipped",
			capMap: tailcfg.PeerCapMap{cap: {`not json`, `{"groups":["admin"]}`}},
			want:   []string{"admin"},
		},
		{
			name: "grant without groups alongside grant with groups",
			capMap: tailcfg.PeerCapMap{cap: {
				`{"access":"rw"}`,
				`{"groups":["editor"]}`,
			}},
			want: []string{"editor"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCapGroups(tt.capMap, cap)
			if len(got) != len(tt.want) {
				t.Fatalf("extractCapGroups() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractCapGroups()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTailscaleProvider_Authenticate_ContextCancelled(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			err: context.Canceled,
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "100.64.0.1:12345"
	// Cancel the context before calling Authenticate.
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	r = r.WithContext(ctx)

	id, err := p.Authenticate(httptest.NewRecorder(), r)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if id != nil {
		t.Fatal("expected nil identity")
	}
}

// spyWhoIsClient wraps a WhoIsClient and records whether WhoIs was called.
type spyWhoIsClient struct {
	inner  WhoIsClient
	called *bool
}

func (s *spyWhoIsClient) WhoIs(
	ctx context.Context,
	remoteAddr string,
) (*apitype.WhoIsResponse, error) {
	*s.called = true
	return s.inner.WhoIs(ctx, remoteAddr)
}
