package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// authCodeTTL is the lifetime of an authorization code issued at the end of
// the consent flow. RFC 6749 §4.1.2 recommends short-lived codes; 60 seconds
// is a common conservative choice.
const authCodeTTL = 60 * time.Second

// ServerConfig holds configuration for the OAuth 2.1 authorization server.
type ServerConfig struct {
	// Issuer is the canonical issuer URL, e.g. "https://cetacean.example.com".
	Issuer string

	// BasePath is an optional URL prefix, e.g. "" or "/cetacean".
	BasePath string

	// MCPResource is the canonical MCP endpoint URL — both the PRM resource
	// identifier and the JWT audience.
	MCPResource string

	// MCP holds DCR knobs and the require_resource_indicator flag.
	MCP config.MCPConfig

	// SigningKey is the HMAC-SHA256 key for JWTs and CSRF tokens. If
	// MCPConfig.SigningKey is empty, main.go auto-generates an ephemeral key.
	SigningKey []byte

	// HTTPClient is an optional HTTP client for CIMD fetches.
	HTTPClient *http.Client

	// TokenStorePath is where refresh tokens are persisted, so a restart does
	// not force every client to re-authorize. Empty keeps the store in memory
	// only, which is what happens when the data directory is not writable.
	TokenStorePath string
}

// Server is the OAuth 2.1 authorization server. Use NewServer to construct.
type Server struct {
	cfg           ServerConfig
	tokenIssuer   *TokenIssuer
	cimd          *CIMDFetcher
	authCodes     *AuthCodeStore
	refreshTokens *RefreshTokenStore
	clients       *ClientRegistry // nil when DCREnabled is false
}

// issuerID is the external base URL clients discover this authorization server
// at — Issuer plus any base path. It is the single source of truth for the
// advertised `issuer` (AS metadata), the PRM `authorization_servers` entry, and
// the token `iss` claim, so all three agree with the path the well-known
// documents and OAuth endpoints are actually served under. With an empty base
// path it is just Issuer.
func (c ServerConfig) issuerID() string {
	return c.Issuer + c.BasePath
}

// NewServer constructs a fully wired Server from cfg. No separate init step
// is required; call RegisterRoutes to attach handlers to a mux.
func NewServer(cfg ServerConfig) *Server {
	issuer := &TokenIssuer{
		SigningKey: cfg.SigningKey,
		Issuer:     cfg.issuerID(),
		Audience:   cfg.MCPResource,
	}

	cimd := &CIMDFetcher{
		Client: cfg.HTTPClient,
	}

	var clients *ClientRegistry
	if cfg.MCP.DCREnabled {
		clients = newClientRegistry(cfg.MCP.DCRMaxClients, cfg.MCP.DCRRateLimit)
	}

	refreshTokens := NewRefreshTokenStore()
	if cfg.TokenStorePath != "" {
		// A missing file is the normal first start. Anything else — corrupt
		// JSON, bad permissions, a version from a newer build — costs every
		// client a re-authorization, so it is worth an operator's attention.
		// Neither is fatal: the server comes up empty and clients re-authorize,
		// exactly as they did before the store existed.
		if state, err := readState(cfg.TokenStorePath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				slog.Info("no MCP OAuth state yet", "path", cfg.TokenStorePath)
			} else {
				slog.Warn(
					"could not read MCP OAuth state; clients must re-authorize",
					"error", err,
					"path", cfg.TokenStorePath,
				)
			}
		} else {
			refreshTokens.Restore(state.RefreshTokenSnapshot)
			slog.Info("loaded MCP OAuth state", "grants", len(state.Grants))
		}

		file := &stateFile{path: cfg.TokenStorePath, tokens: refreshTokens}
		refreshTokens.SetOnChange(file.write)
	}

	return &Server{
		cfg:           cfg,
		tokenIssuer:   issuer,
		cimd:          cimd,
		authCodes:     NewAuthCodeStore(),
		refreshTokens: refreshTokens,
		clients:       clients,
	}
}

// RegisterRoutes attaches all OAuth endpoints to mux under basePath.
func (s *Server) RegisterRoutes(mux *http.ServeMux, basePath string) {
	mux.HandleFunc("GET "+basePath+"/.well-known/oauth-authorization-server", s.HandleMetadata)
	mux.HandleFunc("GET "+basePath+"/.well-known/openid-configuration", s.HandleMetadata)
	mux.HandleFunc(
		"GET "+basePath+"/.well-known/oauth-protected-resource",
		s.HandleProtectedResourceMetadata,
	)
	mux.HandleFunc("GET "+basePath+"/oauth/authorize", s.HandleAuthorize)
	mux.HandleFunc("POST "+basePath+"/oauth/authorize", s.HandleAuthorize)
	mux.HandleFunc("POST "+basePath+"/oauth/token", s.HandleToken)
	mux.HandleFunc("POST "+basePath+"/oauth/revoke", s.HandleRevoke)
	if s.cfg.MCP.DCREnabled {
		mux.HandleFunc("POST "+basePath+"/oauth/register", s.HandleRegister)
	}
}

// ---------------------------------------------------------------------------
// AS Metadata (RFC 8414)
// ---------------------------------------------------------------------------

// asMetadata is the RFC 8414 Authorization Server Metadata document.
type asMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint,omitempty"`

	// ClientIDMetadataDocumentSupported advertises CIMD, which 2026-07-28
	// prefers over RFC 7591 DCR. A client has no other way to learn that an
	// https:// client_id will be accepted. Omitted when CIMD is disabled, so
	// the document never points at a path the server will refuse.
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported,omitempty"`

	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
	GrantTypesSupported                    []string `json:"grant_types_supported"`
	ResponseTypesSupported                 []string `json:"response_types_supported"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
}

// HandleMetadata serves the RFC 8414 AS metadata document.
func (s *Server) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.cfg.issuerID()
	doc := asMetadata{
		Issuer:                                 base,
		AuthorizationEndpoint:                  base + "/oauth/authorize",
		TokenEndpoint:                          base + "/oauth/token",
		RevocationEndpoint:                     base + "/oauth/revoke",
		CodeChallengeMethodsSupported:          []string{"S256"},
		GrantTypesSupported:                    []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:                 []string{"code"},
		TokenEndpointAuthMethodsSupported:      []string{"none"},
		RevocationEndpointAuthMethodsSupported: []string{"none"},
	}
	if s.cfg.MCP.DCREnabled {
		doc.RegistrationEndpoint = base + "/oauth/register"
	}

	doc.ClientIDMetadataDocumentSupported = s.cfg.MCP.CIMDEnabled

	// Marshal first so an encoding failure doesn't write partial headers
	// followed by a 500 status (which would corrupt the response).
	body, err := json.Marshal(doc)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(body)
}

// ---------------------------------------------------------------------------
// Token endpoint (RFC 6749 / OAuth 2.1)
// ---------------------------------------------------------------------------

// oauthErrorResponse is the RFC 6749 §5.2 error response format.
type oauthErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// tokenResponse is the RFC 6749 §5.1 successful token response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// tokenEndpointMaxBytes caps the request body on the token endpoint. RFC 6749
// is silent on size; real grants fit in well under 8 KiB. The cap stops a
// rogue caller from forcing the server to buffer megabytes via ParseForm.
const tokenEndpointMaxBytes = 8 * 1024

// authorizeEndpointMaxBytes caps the form body on POST /oauth/authorize.
// Slightly larger to leave room for redirect_uri and PKCE challenge.
const authorizeEndpointMaxBytes = 16 * 1024

// HandleToken handles POST {base}/oauth/token.
func (s *Server) HandleToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, tokenEndpointMaxBytes)
	if err := r.ParseForm(); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", "cannot parse form body")
		return
	}

	grantType := r.FormValue("grant_type")
	switch grantType {
	case "authorization_code":
		s.handleAuthorizationCodeGrant(w, r)
	case "refresh_token":
		s.handleRefreshTokenGrant(w, r)
	default:
		writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type",
			"grant_type must be authorization_code or refresh_token")
	}
}

func (s *Server) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	// Body size is already capped in HandleToken before ParseForm runs; the
	// values below come from r.PostForm so no further reads of r.Body happen
	// here. Annotated for gosec G120, which inspects callers in isolation.
	code := r.FormValue("code")                  // #nosec G120 -- bounded in HandleToken
	redirectURI := r.FormValue("redirect_uri")   // #nosec G120 -- bounded in HandleToken
	clientID := r.FormValue("client_id")         // #nosec G120 -- bounded in HandleToken
	codeVerifier := r.FormValue("code_verifier") // #nosec G120 -- bounded in HandleToken
	resourceForm := r.FormValue("resource")      // #nosec G120 -- bounded in HandleToken

	// RFC 8707 resource indicator validation.
	if _, err := ValidateResourceIndicator(
		resourceForm,
		s.cfg.MCPResource,
		s.cfg.MCP.RequireResourceIndicator,
	); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}

	// Redeem the authorization code.
	codeData, ok := s.authCodes.Redeem(code)
	if !ok {
		writeTokenError(
			w,
			http.StatusBadRequest,
			"invalid_grant",
			"authorization code is invalid or expired",
		)
		return
	}

	// Match client_id.
	if clientID != codeData.ClientID {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}

	// Match redirect_uri.
	if redirectURI != codeData.RedirectURI {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}

	// Match resource.
	if resourceForm != "" && resourceForm != codeData.Resource {
		writeTokenError(
			w,
			http.StatusBadRequest,
			"invalid_target",
			"resource does not match authorization code",
		)
		return
	}

	// Verify PKCE S256. RFC 7636 §4.1 requires 43–128 characters from the
	// unreserved set (ALPHA / DIGIT / "-" / "." / "_" / "~"). Enforce the
	// length and alphabet here — a 1-character verifier brute-forces in
	// milliseconds against a 43-char base64url challenge, which defeats the
	// point of PKCE.
	if err := validateCodeVerifier(codeVerifier); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}
	if !verifySHA256Challenge(codeVerifier, codeData.CodeChallenge) {
		writeTokenError(
			w,
			http.StatusBadRequest,
			"invalid_grant",
			"code_verifier does not match code_challenge",
		)
		return
	}

	// Issue access token.
	accessToken, err := s.tokenIssuer.IssueAccessToken(AccessTokenClaims{
		Subject:  codeData.Subject,
		Groups:   codeData.Groups,
		ClientID: codeData.ClientID,
	}, s.cfg.MCP.AccessTokenTTL)
	if err != nil {
		writeTokenError(
			w,
			http.StatusInternalServerError,
			"server_error",
			"failed to issue access token",
		)
		return
	}

	// Issue refresh token.
	refreshToken := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  codeData.Subject,
		Groups:   codeData.Groups,
		ClientID: codeData.ClientID,
		Resource: codeData.Resource,
	}, s.cfg.MCP.RefreshTokenTTL)

	writeTokenResponse(w, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.cfg.MCP.AccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
	})
}

func (s *Server) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	// Body bounded by HandleToken; comments suppress gosec G120's
	// per-function analysis.
	refreshTokenRaw := r.FormValue("refresh_token") // #nosec G120 -- bounded in HandleToken
	resourceForm := r.FormValue("resource")         // #nosec G120 -- bounded in HandleToken

	// RFC 8707 resource indicator validation against the server's resource.
	// Run BEFORE consuming the refresh token: a malformed resource parameter
	// (which is almost always a client typo) should not burn the grant family.
	// Theft detection still works because a replay of an already-rotated token
	// triggers Rotate's Theft branch on its second presentation.
	if _, err := ValidateResourceIndicator(
		resourceForm,
		s.cfg.MCPResource,
		s.cfg.MCP.RequireResourceIndicator,
	); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}

	// Confirm the bound resource matches BEFORE rotation, again so a client
	// typo doesn't revoke the entire family.
	if resourceForm != "" {
		bound, ok := s.refreshTokens.Validate(refreshTokenRaw)
		if !ok {
			writeTokenError(
				w,
				http.StatusBadRequest,
				"invalid_grant",
				"refresh token is invalid or expired",
			)
			return
		}
		if resourceForm != bound.Resource {
			writeTokenError(
				w,
				http.StatusBadRequest,
				"invalid_target",
				"resource does not match this grant",
			)
			return
		}
	}

	// Now consume (rotate) the token. Theft detection runs inside Rotate.
	result := s.refreshTokens.Rotate(refreshTokenRaw, s.cfg.MCP.RefreshTokenTTL)
	if result.Theft {
		// Per RFC 6749 §5.2: don't leak that it was theft.
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
		return
	}
	if !result.OK {
		writeTokenError(
			w,
			http.StatusBadRequest,
			"invalid_grant",
			"refresh token is invalid or expired",
		)
		return
	}

	// Issue new access token.
	accessToken, err := s.tokenIssuer.IssueAccessToken(AccessTokenClaims{
		Subject:  result.Data.Subject,
		Groups:   result.Data.Groups,
		ClientID: result.Data.ClientID,
	}, s.cfg.MCP.AccessTokenTTL)
	if err != nil {
		writeTokenError(
			w,
			http.StatusInternalServerError,
			"server_error",
			"failed to issue access token",
		)
		return
	}

	writeTokenResponse(w, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.cfg.MCP.AccessTokenTTL.Seconds()),
		RefreshToken: result.NewToken,
	})
}

// verifySHA256Challenge checks that sha256(verifier) base64url-encodes to challenge.
func verifySHA256Challenge(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return hmac.Equal([]byte(computed), []byte(challenge))
}

// validateCodeVerifier enforces the RFC 7636 §4.1 shape: 43–128 characters
// from the unreserved alphabet.
func validateCodeVerifier(v string) error {
	if n := len(v); n < 43 || n > 128 {
		return fmt.Errorf("code_verifier length %d outside RFC 7636 range [43,128]", n)
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-', c == '.', c == '_', c == '~':
		default:
			return fmt.Errorf("code_verifier contains illegal character at byte %d", i)
		}
	}
	return nil
}

func writeTokenError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(oauthErrorResponse{Error: code, ErrorDescription: desc})
}

func writeTokenResponse(w http.ResponseWriter, resp tokenResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	// The whole point of this endpoint is to return the access token; gosec
	// G117 flags the field name as a "secret in JSON" pattern.
	// #nosec G117 -- RFC 6749 §5.1 token response shape, intentional
	_ = json.NewEncoder(w).Encode(resp)
}

// ---------------------------------------------------------------------------
// Revocation endpoint (RFC 7009)
// ---------------------------------------------------------------------------

// HandleRevoke handles POST {base}/oauth/revoke (RFC 7009).
//
// Limitation: revocation only applies to refresh tokens. Access tokens are
// stateless HMAC JWTs and continue to validate until their `exp` claim
// (default 1h via CETACEAN_MCP_ACCESS_TOKEN_TTL). Per RFC 7009 §2.2 the
// server still returns 200 OK regardless of token type so the client cannot
// distinguish "unknown token" from "no-op". Adding real access-token
// revocation would require a JTI denylist sized to AccessTokenTTL — not
// implemented today because short-lived tokens make this acceptable.
func (s *Server) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, tokenEndpointMaxBytes)
	if err := r.ParseForm(); err != nil {
		// RFC 7009: always 200 even on malformed requests.
		w.WriteHeader(http.StatusOK)
		return
	}
	token := r.FormValue("token") // #nosec G120 -- bounded above
	if token != "" {
		s.refreshTokens.RevokeGrant(token)
	}
	// RFC 7009 §2.2: always 200 regardless of whether the token was valid.
	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// Authorize endpoint
// ---------------------------------------------------------------------------

// HandleAuthorize handles GET and POST {base}/oauth/authorize.
func (s *Server) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAuthorizeGET(w, r)
	case http.MethodPost:
		s.handleAuthorizePOST(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAuthorizeGET(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	responseType := q.Get("response_type")
	clientID := q.Get("client_id")
	redirectURIRaw := q.Get("redirect_uri")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	state := q.Get("state")
	resourceParam := q.Get("resource")

	// Resolve client metadata and validate redirect_uri BEFORE any redirect.
	meta, verified, errMsg := s.resolveClientMeta(r, clientID)
	if errMsg != "" {
		renderErrorPage(w, http.StatusBadRequest, errMsg)
		return
	}

	if !meta.HasRedirectURI(redirectURIRaw) {
		renderErrorPage(w, http.StatusBadRequest,
			"redirect_uri is not registered for this client")
		return
	}

	// From here on redirect_uri is verified — errors can redirect.

	if responseType != "code" {
		s.redirectWithError(w, r, redirectURIRaw, state, "unsupported_response_type",
			"response_type must be code")
		return
	}

	if codeChallenge == "" {
		s.redirectWithError(w, r, redirectURIRaw, state, "invalid_request",
			"code_challenge is required")
		return
	}
	if codeChallengeMethod != "S256" {
		s.redirectWithError(w, r, redirectURIRaw, state, "invalid_request",
			"code_challenge_method must be S256")
		return
	}

	if _, err := ValidateResourceIndicator(
		resourceParam,
		s.cfg.MCPResource,
		s.cfg.MCP.RequireResourceIndicator,
	); err != nil {
		s.redirectWithError(w, r, redirectURIRaw, state, "invalid_target", err.Error())
		return
	}

	// Require identity from auth middleware.
	identity := auth.IdentityFromContext(r.Context())
	if identity == nil {
		renderErrorPage(w, http.StatusUnauthorized, "authentication required")
		return
	}

	csrfToken, _ := issueCSRFNonce(
		w,
		s.cfg.SigningKey,
		state,
		strings.HasPrefix(s.cfg.Issuer, "https://"),
	)

	actionURL := s.cfg.BasePath + "/oauth/authorize"

	renderConsent(w, consentData{
		ClientName:          meta.ClientName,
		Verified:            verified,
		RedirectURI:         redirectURIRaw,
		Subject:             identity.Subject,
		Email:               identity.Email,
		ActionURL:           actionURL,
		ResponseType:        responseType,
		ClientID:            clientID,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		State:               state,
		Resource:            resourceParam,
		CSRFToken:           csrfToken,
	})
}

func (s *Server) handleAuthorizePOST(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, authorizeEndpointMaxBytes)
	if err := r.ParseForm(); err != nil {
		renderErrorPage(w, http.StatusBadRequest, "invalid form submission")
		return
	}

	redirectURIRaw := r.FormValue("redirect_uri")
	clientID := r.FormValue("client_id")
	state := r.FormValue("state")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")
	resourceParam := r.FormValue("resource")
	decision := r.FormValue("decision")

	// Re-validate client and redirect_uri before any redirect.
	meta, _, errMsg := s.resolveClientMeta(r, clientID)
	if errMsg != "" {
		renderErrorPage(w, http.StatusBadRequest, errMsg)
		return
	}
	if !meta.HasRedirectURI(redirectURIRaw) {
		renderErrorPage(w, http.StatusBadRequest,
			"redirect_uri is not registered for this client")
		return
	}

	secure := strings.HasPrefix(s.cfg.Issuer, "https://")

	// Validate CSRF.
	if !verifyCSRFToken(r, s.cfg.SigningKey) {
		renderErrorPage(w, http.StatusBadRequest, "invalid or missing CSRF token")
		return
	}

	if decision == "deny" {
		clearCSRFCookie(w, secure)
		s.redirectWithError(w, r, redirectURIRaw, state, "access_denied",
			"user denied the authorization request")
		return
	}

	// Require identity.
	identity := auth.IdentityFromContext(r.Context())
	if identity == nil {
		clearCSRFCookie(w, secure)
		renderErrorPage(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if codeChallengeMethod != "S256" {
		clearCSRFCookie(w, secure)
		s.redirectWithError(w, r, redirectURIRaw, state, "invalid_request",
			"code_challenge_method must be S256")
		return
	}

	effectiveResource, err := ValidateResourceIndicator(
		resourceParam,
		s.cfg.MCPResource,
		s.cfg.MCP.RequireResourceIndicator,
	)
	if err != nil {
		clearCSRFCookie(w, secure)
		s.redirectWithError(w, r, redirectURIRaw, state, "invalid_target", err.Error())
		return
	}

	// Issue authorization code.
	rawCode := s.authCodes.Issue(AuthCodeData{
		ClientID:      clientID,
		RedirectURI:   redirectURIRaw,
		CodeChallenge: codeChallenge,
		Resource:      effectiveResource,
		Subject:       identity.Subject,
		Groups:        identity.Groups,
	}, authCodeTTL)

	// Clear the CSRF cookie — the flow is complete.
	clearCSRFCookie(w, secure)

	// Redirect with code.
	redirectURI, _ := url.Parse(redirectURIRaw)
	q := redirectURI.Query()
	q.Set("code", rawCode)
	if state != "" {
		q.Set("state", state)
	}
	// RFC 9207: name the issuer in the authorization response so a client
	// configured with several authorization servers cannot be tricked into
	// redeeming this code at the wrong one (mix-up attack).
	q.Set("iss", s.cfg.issuerID())
	redirectURI.RawQuery = q.Encode()
	//nolint:gosec // G710: redirectURIRaw was exact-matched against the client's registered redirect_uris (HasRedirectURI) earlier in handleAuthorizePOST; this is a pre-validated URI, not open redirect.
	http.Redirect(w, r, redirectURI.String(), http.StatusFound)
}

// cimdDisabledMessage is shown when a client presents an https:// client_id
// while CIMD is switched off.
const cimdDisabledMessage = "client ID metadata documents are not enabled"

// resolveClientMeta returns ClientMetadata and verified=true (CIMD) or
// false (DCR), or an error message string if the client cannot be resolved.
func (s *Server) resolveClientMeta(
	r *http.Request,
	clientID string,
) (meta *ClientMetadata, verified bool, errMsg string) {
	if strings.HasPrefix(clientID, "https://") {
		// CIMD makes the server fetch a URL the client chose, so an operator
		// who disabled it is deliberately removing outbound request surface.
		// Refuse before fetching rather than after.
		if !s.cfg.MCP.CIMDEnabled {
			return nil, false, cimdDisabledMessage
		}

		// Don't surface the raw fetcher error to the browser —
		// it can include DNS lookups, SSRF block reasons, "connection refused",
		// etc. Log the specifics for operators and show a generic message.
		m, err := s.cimd.Fetch(r.Context(), clientID)
		if err != nil {
			slog.Warn("MCP CIMD fetch failed",
				"client_id", clientID,
				"error", err,
			)
			return nil, false, "client metadata could not be retrieved"
		}
		return m, true, ""
	}

	// DCR path.
	if s.clients == nil {
		return nil, false, "dynamic client registration is not enabled"
	}
	reg := s.clients.Get(clientID)
	if reg == nil {
		return nil, false, "client not found"
	}
	return &ClientMetadata{
		ClientID:                reg.ClientID,
		ClientName:              reg.ClientName,
		RedirectURIs:            reg.RedirectURIs,
		TokenEndpointAuthMethod: reg.TokenEndpointAuthMethod,
	}, false, ""
}

// redirectWithError sends an OAuth error redirect to redirect_uri.
func (s *Server) redirectWithError(
	w http.ResponseWriter,
	r *http.Request,
	redirectURIRaw, state, code, desc string,
) {
	u, err := url.Parse(redirectURIRaw)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", code)
	if desc != "" {
		q.Set("error_description", desc)
	}
	if state != "" {
		q.Set("state", state)
	}
	// RFC 9207 §2 requires iss on error responses too: a client must be able
	// to attribute the failure before acting on it.
	q.Set("iss", s.cfg.issuerID())
	u.RawQuery = q.Encode()
	//nolint:gosec // G710: callers (handleAuthorizePOST) exact-match redirectURIRaw against the client's registered redirect_uris before invoking this; the target is a pre-validated URI, not open redirect.
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// ---------------------------------------------------------------------------
// WWW-Authenticate helper
// ---------------------------------------------------------------------------

// VerifyAccessToken verifies a JWT signature, issuer, audience and expiry.
// Returns the application claims on success. Exposed so the MCP HTTP handler
// can validate bearer tokens without reaching into the oauth package's
// internals.
func (s *Server) VerifyAccessToken(token string) (*AccessTokenClaims, error) {
	return s.tokenIssuer.VerifyAccessToken(token)
}

// WriteUnauthorized writes a 401 response with a WWW-Authenticate header
// that includes the protected resource metadata URL and the error code.
// Used by the MCP HTTP handler when a bearer token is missing or invalid.
func (s *Server) WriteUnauthorized(w http.ResponseWriter, errorCode string) {
	prmURL := s.cfg.issuerID() + "/.well-known/oauth-protected-resource"
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer realm="mcp", resource_metadata=%s, error=%s`,
		httpQuotedString(prmURL), httpQuotedString(errorCode),
	))
	w.WriteHeader(http.StatusUnauthorized)
}

// httpQuotedString wraps s in an RFC 7230 quoted-string. Per RFC 7230 §3.2.6
// the only characters that must be escaped inside quoted-string are " and \;
// everything else in the visible-ASCII range (and obs-text) is allowed bare.
// Go's %q produces a Go-syntax string literal — close but wrong by spec,
// notably for backticks and non-ASCII runes. We escape "\" and `"` only.
func httpQuotedString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}
