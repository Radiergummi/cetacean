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
	for i := 0; i < 3; i++ {
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

	for i := 0; i < 3; i++ {
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
