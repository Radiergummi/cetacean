package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/config"
)

// newDCRRequest builds a POST /oauth/register request with the given JSON body.
func newDCRRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:50000"
	return req
}

// ---------------------------------------------------------------------------
// TestDCRRegister — happy path
// ---------------------------------------------------------------------------

func TestDCRRegister(t *testing.T) {
	s := newTestServer(t)

	body := `{
		"client_name": "Claude Code",
		"redirect_uris": ["http://localhost:33418/callback"],
		"grant_types": ["authorization_code","refresh_token"],
		"response_types": ["code"],
		"token_endpoint_auth_method": "none"
	}`
	req := newDCRRequest(t, body)
	rec := httptest.NewRecorder()
	s.HandleRegister(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var reg ClientRegistration
	if err := json.NewDecoder(rec.Body).Decode(&reg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(reg.ClientID, "cetacean-") {
		t.Errorf("client_id = %q, want cetacean-... prefix", reg.ClientID)
	}
	if reg.ClientIDIssuedAt == 0 {
		t.Error("client_id_issued_at must be set")
	}
	if len(reg.RedirectURIs) == 0 || reg.RedirectURIs[0] != "http://localhost:33418/callback" {
		t.Errorf("redirect_uris = %v", reg.RedirectURIs)
	}

	// Must be retrievable.
	fetched := s.clients.Get(reg.ClientID)
	if fetched == nil {
		t.Fatal("registered client not retrievable")
	}
}

// ---------------------------------------------------------------------------
// TestDCRRejectsSymmetricAuth
// ---------------------------------------------------------------------------

func TestDCRRejectsSymmetricAuth(t *testing.T) {
	s := newTestServer(t)

	body := `{
		"redirect_uris": ["http://localhost/cb"],
		"token_endpoint_auth_method": "client_secret_post"
	}`
	rec := httptest.NewRecorder()
	s.HandleRegister(rec, newDCRRequest(t, body))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	var errResp dcrErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errResp.Error != "invalid_client_metadata" {
		t.Errorf("error = %q, want invalid_client_metadata", errResp.Error)
	}
}

// ---------------------------------------------------------------------------
// TestDCRRejectsUnsupportedGrantType
// ---------------------------------------------------------------------------

func TestDCRRejectsUnsupportedGrantType(t *testing.T) {
	s := newTestServer(t)

	body := `{
		"redirect_uris": ["http://localhost/cb"],
		"grant_types": ["authorization_code", "client_credentials"]
	}`
	rec := httptest.NewRecorder()
	s.HandleRegister(rec, newDCRRequest(t, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp dcrErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errResp.Error != "invalid_client_metadata" {
		t.Errorf("error = %q, want invalid_client_metadata", errResp.Error)
	}
}

// ---------------------------------------------------------------------------
// TestDCRRejectsUnsupportedResponseType
// ---------------------------------------------------------------------------

func TestDCRRejectsUnsupportedResponseType(t *testing.T) {
	s := newTestServer(t)

	body := `{
		"redirect_uris": ["http://localhost/cb"],
		"response_types": ["token"]
	}`
	rec := httptest.NewRecorder()
	s.HandleRegister(rec, newDCRRequest(t, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp dcrErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errResp.Error != "invalid_client_metadata" {
		t.Errorf("error = %q, want invalid_client_metadata", errResp.Error)
	}
}

// ---------------------------------------------------------------------------
// TestDCRMissingRedirectURIs
// ---------------------------------------------------------------------------

func TestDCRMissingRedirectURIs(t *testing.T) {
	s := newTestServer(t)

	body := `{"client_name": "App"}`
	rec := httptest.NewRecorder()
	s.HandleRegister(rec, newDCRRequest(t, body))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// TestDCRRateLimit
// ---------------------------------------------------------------------------

func TestDCRRateLimit(t *testing.T) {
	// Configure a small rate limit (3/hour) for testing.
	cfg := ServerConfig{
		Issuer:      "https://cetacean.test",
		MCPResource: "https://cetacean.test/mcp",
		MCP: config.MCPConfig{
			AccessTokenTTL:  10 * 60 * 1e9, // 10m in nanoseconds
			RefreshTokenTTL: 720 * 3600 * 1e9,
			DCREnabled:      true,
			DCRRateLimit:    3,
			DCRMaxClients:   100,
		},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
	}
	s := NewServer(cfg)

	validBody := `{"client_name":"App","redirect_uris":["http://localhost/cb"]}`

	// First 3 requests should succeed.
	for i := range 3 {
		req := newDCRRequest(t, validBody)
		rec := httptest.NewRecorder()
		s.HandleRegister(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("request %d: expected 201, got %d: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	// 4th request must be rate-limited.
	req := newDCRRequest(t, validBody)
	rec := httptest.NewRecorder()
	s.HandleRegister(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 on rate limit, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429")
	}
}

// ---------------------------------------------------------------------------
// TestDCRLRUEviction
// ---------------------------------------------------------------------------

func TestDCRLRUEviction(t *testing.T) {
	// Max 2 clients.
	cfg := ServerConfig{
		Issuer:      "https://cetacean.test",
		MCPResource: "https://cetacean.test/mcp",
		MCP: config.MCPConfig{
			AccessTokenTTL:  3600 * 1e9,
			RefreshTokenTTL: 720 * 3600 * 1e9,
			DCREnabled:      true,
			DCRRateLimit:    100,
			DCRMaxClients:   2,
		},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
	}
	s := NewServer(cfg)

	validBody := `{"client_name":"App","redirect_uris":["http://localhost/cb"]}`
	var ids []string

	for i := range 3 {
		req := newDCRRequest(t, validBody)
		req.RemoteAddr = "10.0.0.1:1234" // same IP, high rate limit
		rec := httptest.NewRecorder()
		s.HandleRegister(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("register %d: expected 201, got %d: %s", i, rec.Code, rec.Body.String())
		}
		var reg ClientRegistration
		if err := json.NewDecoder(rec.Body).Decode(&reg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		ids = append(ids, reg.ClientID)
	}

	// The first registered client should be evicted.
	if s.clients.Get(ids[0]) != nil {
		t.Error("first client should have been evicted by LRU")
	}
	// The second and third should still be present.
	if s.clients.Get(ids[1]) == nil {
		t.Error("second client should still be present")
	}
	if s.clients.Get(ids[2]) == nil {
		t.Error("third client should still be present")
	}
}

// registerClient posts a registration body and returns the status and decoded
// registration. It does not assert success — callers checking a rejection need
// the failing status.
func registerClient(t *testing.T, s *Server, body string) (int, ClientRegistration) {
	t.Helper()

	rec := httptest.NewRecorder()
	s.HandleRegister(rec, newDCRRequest(t, body))

	var reg ClientRegistration
	if rec.Code == http.StatusCreated {
		if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil {
			t.Fatalf("decode registration: %v (body %s)", err, rec.Body.String())
		}
	}

	return rec.Code, reg
}

// TestDCRDefaultsApplicationTypeToNative — SEP-837. OpenID Connect defaults
// application_type to "web", which forbids the loopback redirect URIs native
// MCP clients use. Defaulting to "native" avoids rejecting a correct client
// that simply did not send the field.
func TestDCRDefaultsApplicationTypeToNative(t *testing.T) {
	s := newTestServer(t)

	status, reg := registerClient(t, s, `{
		"client_name": "Native client",
		"redirect_uris": ["http://127.0.0.1:49152/cb"]
	}`)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", status)
	}

	if reg.ApplicationType != "native" {
		t.Fatalf("application_type = %q, want %q", reg.ApplicationType, "native")
	}
}

func TestDCREchoesExplicitApplicationType(t *testing.T) {
	s := newTestServer(t)

	status, reg := registerClient(t, s, `{
		"client_name": "Web client",
		"redirect_uris": ["https://client.example/cb"],
		"application_type": "web"
	}`)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", status)
	}

	if reg.ApplicationType != "web" {
		t.Fatalf("application_type = %q, want %q", reg.ApplicationType, "web")
	}
}

func TestDCRRejectsUnknownApplicationType(t *testing.T) {
	s := newTestServer(t)

	status, _ := registerClient(t, s, `{
		"redirect_uris": ["https://client.example/cb"],
		"application_type": "carrier-pigeon"
	}`)
	if status == http.StatusCreated {
		t.Fatal("registration accepted an unknown application_type")
	}
}

// TestDCRWebApplicationTypeRejectsLoopbackRedirect is the reason the field
// exists: a "web" client must not register a loopback redirect URI.
func TestDCRWebApplicationTypeRejectsLoopbackRedirect(t *testing.T) {
	s := newTestServer(t)

	status, _ := registerClient(t, s, `{
		"redirect_uris": ["http://127.0.0.1:49152/cb"],
		"application_type": "web"
	}`)
	if status == http.StatusCreated {
		t.Fatal("a web client registered a loopback redirect URI")
	}
}

// TestDCRNativeApplicationTypeAllowsLoopback guards the other direction — the
// default must keep working for the clients it exists for.
func TestDCRNativeApplicationTypeAllowsLoopback(t *testing.T) {
	s := newTestServer(t)

	status, _ := registerClient(t, s, `{
		"redirect_uris": ["http://localhost:33418/callback"],
		"application_type": "native"
	}`)
	if status != http.StatusCreated {
		t.Fatalf("native client with a loopback redirect was rejected: status %d", status)
	}
}
