package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

// NewServer constructs a fully wired Server from cfg. No separate init step
// is required; call RegisterRoutes to attach handlers to a mux.
func NewServer(cfg ServerConfig) *Server {
	issuer := &TokenIssuer{
		SigningKey: cfg.SigningKey,
		Issuer:    cfg.Issuer,
		Audience:  cfg.MCPResource,
	}

	cimd := &CIMDFetcher{
		Client: cfg.HTTPClient,
	}

	var clients *ClientRegistry
	if cfg.MCP.DCREnabled {
		clients = newClientRegistry(cfg.MCP.DCRMaxClients, cfg.MCP.DCRRateLimit)
	}

	return &Server{
		cfg:           cfg,
		tokenIssuer:   issuer,
		cimd:          cimd,
		authCodes:     NewAuthCodeStore(),
		refreshTokens: NewRefreshTokenStore(),
		clients:       clients,
	}
}

// RegisterRoutes attaches all OAuth endpoints to mux under basePath.
func (s *Server) RegisterRoutes(mux *http.ServeMux, basePath string) {
	mux.HandleFunc("GET "+basePath+"/.well-known/oauth-authorization-server", s.HandleMetadata)
	mux.HandleFunc("GET "+basePath+"/.well-known/oauth-protected-resource", s.HandleProtectedResourceMetadata)
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
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

// HandleMetadata serves the RFC 8414 AS metadata document.
func (s *Server) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.cfg.Issuer + s.cfg.BasePath
	doc := asMetadata{
		Issuer:                        s.cfg.Issuer,
		AuthorizationEndpoint:         base + "/oauth/authorize",
		TokenEndpoint:                 base + "/oauth/token",
		RevocationEndpoint:            base + "/oauth/revoke",
		CodeChallengeMethodsSupported: []string{"S256"},
		GrantTypesSupported:           []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:        []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	}
	if s.cfg.MCP.DCREnabled {
		doc.RegistrationEndpoint = base + "/oauth/register"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=3600")
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
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

// HandleToken handles POST {base}/oauth/token.
func (s *Server) HandleToken(w http.ResponseWriter, r *http.Request) {
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
	code := r.FormValue("code")
	redirectURI := r.FormValue("redirect_uri")
	clientID := r.FormValue("client_id")
	codeVerifier := r.FormValue("code_verifier")
	resourceForm := r.FormValue("resource")

	// RFC 8707 resource indicator validation.
	if _, err := ValidateResourceIndicator(resourceForm, s.cfg.MCPResource, s.cfg.MCP.RequireResourceIndicator); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}

	// Redeem the authorization code.
	codeData, ok := s.authCodes.Redeem(code)
	if !ok {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid or expired")
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
		writeTokenError(w, http.StatusBadRequest, "invalid_target", "resource does not match authorization code")
		return
	}

	// Verify PKCE S256.
	if !verifySHA256Challenge(codeVerifier, codeData.CodeChallenge) {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}

	// Issue access token.
	accessToken, err := s.tokenIssuer.IssueAccessToken(AccessTokenClaims{
		Subject:  codeData.Subject,
		Groups:   codeData.Groups,
		ClientID: codeData.ClientID,
	}, s.cfg.MCP.AccessTokenTTL)
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
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
	refreshTokenRaw := r.FormValue("refresh_token")
	resourceForm := r.FormValue("resource")

	// Rotate the refresh token FIRST so it is consumed before any validation
	// that might need to revoke the grant family (e.g. resource mismatch).
	result := s.refreshTokens.Rotate(refreshTokenRaw, s.cfg.MCP.RefreshTokenTTL)
	if result.Theft {
		// Per RFC 6749 §5.2: don't leak that it was theft.
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
		return
	}
	if !result.OK {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid or expired")
		return
	}

	// RFC 8707 resource indicator validation against the server's resource.
	// Validate after rotation so a mismatched resource triggers grant revocation.
	if _, err := ValidateResourceIndicator(resourceForm, s.cfg.MCPResource, s.cfg.MCP.RequireResourceIndicator); err != nil {
		s.refreshTokens.RevokeGrant(result.NewToken)
		writeTokenError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}

	// Match resource to the grant's bound resource.
	if resourceForm != "" && resourceForm != result.Data.Resource {
		// Resource mismatch: revoke the family and return error.
		s.refreshTokens.RevokeGrant(result.NewToken)
		writeTokenError(w, http.StatusBadRequest, "invalid_target", "resource does not match this grant")
		return
	}

	// Issue new access token.
	accessToken, err := s.tokenIssuer.IssueAccessToken(AccessTokenClaims{
		Subject:  result.Data.Subject,
		Groups:   result.Data.Groups,
		ClientID: result.Data.ClientID,
	}, s.cfg.MCP.AccessTokenTTL)
	if err != nil {
		writeTokenError(w, http.StatusInternalServerError, "server_error", "failed to issue access token")
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
	_ = json.NewEncoder(w).Encode(resp)
}

// ---------------------------------------------------------------------------
// Revocation endpoint (RFC 7009)
// ---------------------------------------------------------------------------

// HandleRevoke handles POST {base}/oauth/revoke.
func (s *Server) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		// RFC 7009: always 200 even on malformed requests.
		w.WriteHeader(http.StatusOK)
		return
	}
	token := r.FormValue("token")
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
		redirectWithError(w, r, redirectURIRaw, state, "unsupported_response_type",
			"response_type must be code")
		return
	}

	if codeChallenge == "" {
		redirectWithError(w, r, redirectURIRaw, state, "invalid_request",
			"code_challenge is required")
		return
	}
	if codeChallengeMethod != "S256" {
		redirectWithError(w, r, redirectURIRaw, state, "invalid_request",
			"code_challenge_method must be S256")
		return
	}

	if _, err := ValidateResourceIndicator(resourceParam, s.cfg.MCPResource, s.cfg.MCP.RequireResourceIndicator); err != nil {
		redirectWithError(w, r, redirectURIRaw, state, "invalid_target", err.Error())
		return
	}

	// Require identity from auth middleware.
	identity := auth.IdentityFromContext(r.Context())
	if identity == nil {
		renderErrorPage(w, http.StatusUnauthorized, "authentication required")
		return
	}

	csrfToken, _ := issueCSRFNonce(w, s.cfg.SigningKey, state, strings.HasPrefix(s.cfg.Issuer, "https://"))

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
		redirectWithError(w, r, redirectURIRaw, state, "access_denied",
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
		redirectWithError(w, r, redirectURIRaw, state, "invalid_request",
			"code_challenge_method must be S256")
		return
	}

	effectiveResource, err := ValidateResourceIndicator(resourceParam, s.cfg.MCPResource, s.cfg.MCP.RequireResourceIndicator)
	if err != nil {
		clearCSRFCookie(w, secure)
		redirectWithError(w, r, redirectURIRaw, state, "invalid_target", err.Error())
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
	redirectURI.RawQuery = q.Encode()
	http.Redirect(w, r, redirectURI.String(), http.StatusFound)
}

// resolveClientMeta returns ClientMetadata and verified=true (CIMD) or
// false (DCR), or an error message string if the client cannot be resolved.
func (s *Server) resolveClientMeta(r *http.Request, clientID string) (meta *ClientMetadata, verified bool, errMsg string) {
	if strings.HasPrefix(clientID, "https://") {
		// CIMD path. Don't surface the raw fetcher error to the browser —
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
func redirectWithError(w http.ResponseWriter, r *http.Request, redirectURIRaw, state, code, desc string) {
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
	u.RawQuery = q.Encode()
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
	prmURL := s.cfg.Issuer + s.cfg.BasePath + "/.well-known/oauth-protected-resource"
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
