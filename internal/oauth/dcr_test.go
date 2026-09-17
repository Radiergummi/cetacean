package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/config"

	"github.com/radiergummi/cetacean/internal/spec"
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
	spec.Satisfies(t,
		"oauth/rfc7591/metadata-fields-are-optional",
		"oauth/rfc7591/redirect-uris-metadata-supported",
		"oauth/rfc7591/open-registration-allowed",
		"oauth/rfc7591/client-id-required",
		"oauth/rfc7591/client-id-issued-at-optional",
		"oauth/rfc7591/public-clients-may-register",
	)

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
	spec.Satisfies(t,
		"oauth/rfc7591/error-code-required",
		"oauth/rfc7591/error-description-optional",
		"oauth/oauth-2-1/client-authentication-needs-confidential-credentials",
	)

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
	if errResp.ErrorDescription == "" {
		t.Error("error_description is absent; the refusal says only that it happened")
	}
}

// ---------------------------------------------------------------------------
// TestDCRRejectsUnsupportedGrantType
// ---------------------------------------------------------------------------

func TestDCRRejectsUnsupportedGrantType(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc7591/inconsistent-registration-is-refused",
		"oauth/oauth-2-1/client-credentials-grant-not-offered",
	)

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
	spec.Satisfies(t, "oauth/rfc7591/inconsistent-registration-is-refused")

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
	spec.Satisfies(t,
		"oauth/rfc7591/redirect-uris-must-be-registered",
		"oauth/oauth-2-1/redirect-uri-registration-stands-in-for-authentication",
	)

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
	spec.Satisfies(t, "oauth/rfc7591/registration-may-be-rate-limited")

	// Configure a small rate limit (3/hour) for testing.
	cfg := ServerConfig{
		Issuer:    "https://cetacean.test",
		Resources: []Resource{{Path: "/resource", Realm: "cetacean"}},
		OAuth: config.OAuthConfig{
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
		Issuer:    "https://cetacean.test",
		Resources: []Resource{{Path: "/resource", Realm: "cetacean"}},
		OAuth: config.OAuthConfig{
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
// these clients use. Defaulting to "native" avoids rejecting a correct client
// that simply did not send the field.
func TestDCRDefaultsApplicationTypeToNative(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc7591/defaults-may-be-provisioned",
		"oauth/rfc7591/all-registered-metadata-returned",
	)

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
	spec.Satisfies(t,
		"oauth/rfc8252/client-type-recorded",
		"oauth/rfc7591/response-may-carry-extension-fields",
		"oauth/oauth-2-1/one-client-id-is-one-client-type",
	)

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
	spec.Satisfies(t, "oauth/rfc8252/client-type-recorded")

	s := newTestServer(t)

	status, _ := registerClient(t, s, `{
		"redirect_uris": ["http://localhost:33418/callback"],
		"application_type": "native"
	}`)
	if status != http.StatusCreated {
		t.Fatalf("native client with a loopback redirect was rejected: status %d", status)
	}
}

// RFC 7591 §2 requires metadata the server does not understand to be ignored,
// which is what lets a client send a richer registration than this endpoint
// reads — including the software statement §3.1.1 permits ignoring outright.
func TestDCRIgnoresUnrecognisedClientMetadata(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc7591/unknown-metadata-is-ignored",
		"oauth/rfc7591/software-statement-may-be-ignored",
	)

	s := newTestServer(t)

	status, reg := registerClient(t, s, `{
		"client_name": "Rich client",
		"redirect_uris": ["https://client.example/cb"],
		"logo_uri": "https://client.example/logo.png",
		"contacts": ["ops@client.example"],
		"software_id": "4NRB1-0XZABZI9E6-5SM3R",
		"software_statement": "eyJhbGciOiJSUzI1NiJ9.e30.c2ln",
		"invented_by_nobody": {"nested": true}
	}`)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201: metadata this server does not read was refused", status)
	}
	if reg.ClientName != "Rich client" {
		t.Errorf("client_name = %q, want %q", reg.ClientName, "Rich client")
	}
}

// RFC 7591 §3.2.1 wants a client_id that is not currently valid for any other
// registered client.
func TestDCRMintsADistinctClientIDPerRegistration(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc7591/client-id-unique-per-client",
		"oauth/rfc9700/client-id-not-chosen-by-the-client",
	)

	s := newTestServer(t)

	const body = `{"redirect_uris": ["https://client.example/cb"]}`

	_, first := registerClient(t, s, body)
	_, second := registerClient(t, s, body)

	if first.ClientID == "" || first.ClientID == second.ClientID {
		t.Errorf("two registrations share client_id %q", first.ClientID)
	}
}

// RFC 7591 §5 limits a registered redirection URI to a TLS-protected site, a
// site on the local machine, or a non-HTTP application-specific URL. Plain
// HTTP anywhere else is none of the three.
func TestDCRRefusesAPlainHTTPRedirectOffLoopback(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc7591/redirect-uri-forms-are-limited",
		"oauth/rfc9700/http-redirect-uris-refused-except-loopback",
	)

	s := newTestServer(t)

	status, _ := registerClient(t, s, `{
		"redirect_uris": ["http://client.example/cb"]
	}`)
	if status == http.StatusCreated {
		t.Error("a cleartext redirect URI off the local machine was registered")
	}
}

// RFC 7591 §3 puts registration behind a transport-layer security mechanism.
// This pins the divergence: the handler registers a client over whatever
// transport reached it, because TLS is the deployment's to terminate.
func TestDCRRegistersOverAPlaintextRequest(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7591/registration-endpoint-requires-tls")

	s := newTestServer(t)

	req := newDCRRequest(t, `{"redirect_uris": ["https://client.example/cb"]}`)
	if req.TLS != nil {
		t.Fatal("the request under test is not the cleartext one")
	}

	rec := httptest.NewRecorder()
	s.HandleRegister(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; the deferral no longer describes the code", rec.Code)
	}
}

// RFC 7591 §3 fixes the method and the media type the registration endpoint
// answers on, so the route has to spell both out.
func TestDCRRouteAcceptsAJSONPost(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7591/registration-accepts-json-post")

	s := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, "")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newDCRRequest(t, `{"redirect_uris": ["https://client.example/cb"]}`))

	if rec.Code != http.StatusCreated {
		t.Errorf("POST /oauth/register = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}
