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

	// Resources are the protected resources this server issues tokens for.
	// The first is the default a token request carrying no RFC 8707 resource
	// indicator resolves to. Empty means one resource at the deployment root.
	Resources []Resource

	// OAuth holds the server's own settings: TTLs, DCR knobs, CIMD and the
	// require_resource_indicator flag.
	OAuth config.OAuthConfig

	// SigningKey is the root the token and CSRF keys derive from. An empty one
	// leaves the server unable to issue tokens.
	SigningKey []byte

	// HTTPClient is an optional HTTP client for CIMD fetches.
	HTTPClient *http.Client

	// StatePath is where the OAuth server's durable state — refresh tokens and
	// remembered approvals — is persisted, so a restart does not force every
	// client to re-authorize. Empty keeps both stores in memory only, which is
	// what happens when the data directory is not writable.
	StatePath string
}

// Server is the OAuth 2.1 authorization server. Use NewServer to construct.
type Server struct {
	cfg           ServerConfig
	tokenIssuer   *TokenIssuer
	cimd          *CIMDFetcher
	authCodes     *AuthCodeStore
	refreshTokens *RefreshTokenStore
	consent       *ConsentStore
	clients       *ClientRegistry // nil when DCREnabled is false
	keys          *keyMaterial    // nil when no root was configured
	resources     resourceSet
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
	// A server configured with no resource protects the deployment root, which is
	// what a single unnamed protected resource is. Defaulted here so every reader
	// downstream can take the set as given.
	if len(cfg.Resources) == 0 {
		cfg.Resources = []Resource{{Realm: "cetacean"}}
	}
	for i := range cfg.Resources {
		cfg.Resources[i].Path = config.NormalizeBasePath(cfg.Resources[i].Path)
	}

	// Without a root, issuing and verifying answer ErrMissingKey.
	km, err := deriveKeys(cfg.SigningKey)
	if err != nil {
		slog.Warn("OAuth has no signing key; tokens cannot be issued", "error", err)
	}

	var issuer *TokenIssuer
	if km != nil {
		issuer = newTokenIssuer(km, cfg.issuerID())
	} else {
		issuer = &TokenIssuer{Issuer: cfg.issuerID()}
	}

	cimd := &CIMDFetcher{
		Client: cfg.HTTPClient,
	}

	var clients *ClientRegistry
	if cfg.OAuth.DCREnabled {
		clients = newClientRegistry(cfg.OAuth.DCRMaxClients, cfg.OAuth.DCRRateLimit)
	}

	refreshTokens := NewRefreshTokenStore()
	consent := NewConsentStore(cfg.OAuth.ConsentTTL)

	if cfg.StatePath != "" {
		sweepTempFiles(cfg.StatePath)

		// A missing file is the normal first start. Anything else — corrupt
		// JSON, bad permissions, a version this build does not write — costs every
		// client a re-authorization, so it is worth an operator's attention.
		// Neither is fatal: the server comes up empty and clients re-authorize,
		// exactly as they did before the store existed.
		state, err := readState(cfg.StatePath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				slog.Info("no OAuth state yet", "path", cfg.StatePath)
			} else {
				slog.Warn(
					"could not read OAuth state; clients must re-authorize",
					"error", err,
					"path", cfg.StatePath,
				)
			}
		} else {
			refreshTokens.Restore(state.RefreshTokenSnapshot)
			consent.Restore(state.Consent)
			slog.Info("loaded OAuth state",
				"grants", len(state.Grants),
				"approvals", len(state.Consent),
			)
		}

		file := &stateFile{path: cfg.StatePath, tokens: refreshTokens, consent: consent}
		refreshTokens.SetOnChange(file.write)
		consent.SetOnChange(file.write)
	}

	return &Server{
		cfg:           cfg,
		tokenIssuer:   issuer,
		cimd:          cimd,
		authCodes:     NewAuthCodeStore(),
		refreshTokens: refreshTokens,
		consent:       consent,
		clients:       clients,
		keys:          km,
		resources:     newResourceSet(cfg),
	}
}

// RegisterRoutes attaches all OAuth endpoints to mux under basePath.
func (s *Server) RegisterRoutes(mux *http.ServeMux, basePath string) {
	// RFC 8414 §3 inserts the well-known segment after the authority, like RFC 9728
	// §3.1, so the AS document gets both spellings for the same reason the resource
	// documents do. OIDC Discovery is a different rule — it appends its suffix to
	// the issuer — so openid-configuration keeps the mounted form only.
	const asMetadataPath = "/.well-known/oauth-authorization-server"

	mux.HandleFunc("GET "+asMetadataPath+s.cfg.BasePath, s.HandleMetadata)
	if mounted := basePath + asMetadataPath; mounted != asMetadataPath+s.cfg.BasePath {
		mux.HandleFunc("GET "+mounted, s.HandleMetadata)
	}

	mux.HandleFunc("GET "+basePath+"/.well-known/openid-configuration", s.HandleMetadata)
	for _, resource := range s.cfg.Resources {
		handler := s.protectedResourceMetadataHandler(resource)

		// Two locations, one document. The first is what RFC 9728 §3.1 derives
		// from the identifier; the second is beneath this deployment's prefix,
		// where a proxy that forwards only that prefix can reach it. They are the
		// same path when there is no base path, so only register once.
		conformant := resource.metadataPath(s.cfg.BasePath)
		mux.HandleFunc("GET "+conformant, handler)

		if mounted := resource.mountedMetadataPath(basePath); mounted != conformant {
			mux.HandleFunc("GET "+mounted, handler)
		}
	}
	mux.HandleFunc("GET "+basePath+jwksPath, s.HandleJWKS)
	mux.HandleFunc("GET "+basePath+"/oauth/authorize", s.HandleAuthorize)
	mux.HandleFunc("POST "+basePath+"/oauth/authorize", s.HandleAuthorize)
	mux.HandleFunc("POST "+basePath+"/oauth/token", s.HandleToken)
	mux.HandleFunc("POST "+basePath+"/oauth/revoke", s.HandleRevoke)
	if s.cfg.OAuth.DCREnabled {
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

	// Omitted with no key to serve, so the document never names an endpoint
	// that would refuse.
	JWKSURI string `json:"jwks_uri,omitempty"`

	// ClientIDMetadataDocumentSupported advertises CIMD, which 2026-07-28
	// prefers over RFC 7591 DCR. A client has no other way to learn that an
	// https:// client_id will be accepted. Omitted when CIMD is disabled, so
	// the document never points at a path the server will refuse.
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported,omitempty"`

	// RFC 9207 §2.4 makes a client's iss check conditional on the server saying it
	// sends one. Without this the mix-up defence every authorization response
	// already carries is invisible, so a conformant client never enforces it.
	AuthorizationResponseIssParameterSupported bool `json:"authorization_response_iss_parameter_supported"`

	// Empty, and present rather than omitted: RFC 8414 §2 recommends the field,
	// and an absent one reads as "unspecified" where an empty array says there are
	// none to ask for. A client that consults it then sends no scope at all rather
	// than guessing at one this server would ignore.
	ScopesSupported []string `json:"scopes_supported"`

	CodeChallengeMethodsSupported          []string `json:"code_challenge_methods_supported"`
	GrantTypesSupported                    []string `json:"grant_types_supported"`
	ResponseTypesSupported                 []string `json:"response_types_supported"`
	TokenEndpointAuthMethodsSupported      []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported []string `json:"revocation_endpoint_auth_methods_supported"`
}

// Marshals before touching the response, so an encoding failure cannot leave
// partial headers in front of a 500.
func writeDiscoveryDoc(w http.ResponseWriter, doc any, contentType string) {
	body, err := json.Marshal(doc)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(body)
}

// HandleMetadata serves the RFC 8414 AS metadata document.
func (s *Server) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.cfg.issuerID()
	doc := asMetadata{
		Issuer:                base,
		AuthorizationEndpoint: base + "/oauth/authorize",
		TokenEndpoint:         base + "/oauth/token",
		RevocationEndpoint:    base + "/oauth/revoke",
		AuthorizationResponseIssParameterSupported: true,

		ScopesSupported: []string{},

		CodeChallengeMethodsSupported:          []string{"S256"},
		GrantTypesSupported:                    []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:                 []string{"code"},
		TokenEndpointAuthMethodsSupported:      []string{"none"},
		RevocationEndpointAuthMethodsSupported: []string{"none"},
	}
	if s.cfg.OAuth.DCREnabled {
		doc.RegistrationEndpoint = base + "/oauth/register"
	}
	if s.keys != nil {
		doc.JWKSURI = base + jwksPath
	}

	doc.ClientIDMetadataDocumentSupported = s.cfg.OAuth.CIMDEnabled

	writeDiscoveryDoc(w, doc, "application/json")
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
	case "":
		// A missing parameter, not an unknown value: only a grant type that is
		// present and unsupported gets unsupported_grant_type.
		writeTokenError(w, http.StatusBadRequest, "invalid_request",
			"grant_type is required")
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
	resourceAll := r.Form["resource"]            // RFC 8707 §2 allows a repeat

	// RFC 8707 resource indicator validation.
	if _, err := s.resources.effectiveResource(
		resourceAll,
		s.cfg.OAuth.RequireResourceIndicator,
	); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}

	if code == "" {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", "code is required")
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

	// Match resource, on the spelling a grant binds to rather than whichever
	// equivalent one the client sent.
	if resourceForm != "" &&
		s.resources.canonicalSpelling(resourceForm) !=
			s.resources.canonicalSpelling(codeData.Resource) {
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
		Subject:     codeData.Subject,
		Email:       codeData.Email,
		DisplayName: codeData.DisplayName,
		Groups:      codeData.Groups,
		ClientID:    codeData.ClientID,
	}, codeData.Resource, s.cfg.OAuth.AccessTokenTTL)
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
		Subject:     codeData.Subject,
		Email:       codeData.Email,
		DisplayName: codeData.DisplayName,
		Groups:      codeData.Groups,
		ClientID:    codeData.ClientID,
		Resource:    codeData.Resource,
	}, s.cfg.OAuth.RefreshTokenTTL)

	// No scope in the response, and none read from the request. RFC 6749 §5.1
	// requires the parameter only when the granted scope differs from the
	// requested one; this server defines none, so both reduce to the empty set.
	// Adding a scope means revisiting that, and scopes_supported with it.
	writeTokenResponse(w, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.cfg.OAuth.AccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
	})
}

func (s *Server) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	// Body bounded by HandleToken; comments suppress gosec G120's
	// per-function analysis.
	refreshTokenRaw := r.FormValue("refresh_token") // #nosec G120 -- bounded in HandleToken
	resourceForm := r.FormValue("resource")         // #nosec G120 -- bounded in HandleToken
	resourceAll := r.Form["resource"]               // RFC 8707 §2 allows a repeat
	clientID := r.FormValue("client_id")            // #nosec G120 -- bounded in HandleToken

	// RFC 8707 resource indicator validation against the server's resource.
	// Run BEFORE consuming the refresh token: a malformed resource parameter
	// (which is almost always a client typo) should not burn the grant family.
	// Theft detection still works because a replay of an already-rotated token
	// triggers Rotate's Theft branch on its second presentation.
	if _, err := s.resources.effectiveResource(
		resourceAll,
		s.cfg.OAuth.RequireResourceIndicator,
	); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_target", err.Error())
		return
	}

	if refreshTokenRaw == "" {
		writeTokenError(
			w,
			http.StatusBadRequest,
			"invalid_request",
			"refresh_token is required",
		)

		return
	}

	// Confirm the bound resource and client match BEFORE rotation, again so a
	// client typo doesn't revoke the entire family. Every client here is public
	// and unauthenticated, so client_id proves nothing against a caller holding
	// the token: RFC 6749 §6 conformance, not an attack worth catching.
	if resourceForm != "" || clientID != "" {
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
		if resourceForm != "" &&
			s.resources.canonicalSpelling(resourceForm) !=
				s.resources.canonicalSpelling(bound.Resource) {
			writeTokenError(
				w,
				http.StatusBadRequest,
				"invalid_target",
				"resource does not match this grant",
			)
			return
		}
		if clientID != "" && clientID != bound.ClientID {
			writeTokenError(
				w,
				http.StatusBadRequest,
				"invalid_grant",
				"client_id does not match this grant",
			)
			return
		}
	}

	// Now consume (rotate) the token. Theft detection runs inside Rotate.
	result := s.refreshTokens.Rotate(refreshTokenRaw, s.cfg.OAuth.RefreshTokenTTL)
	if result.Theft {
		// A replayed token means someone else holds a copy. Re-prompting is
		// the point: the next authorization must reach a human.
		s.consent.Forget(result.Data.ConsentKey())

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
		Subject:     result.Data.Subject,
		Email:       result.Data.Email,
		DisplayName: result.Data.DisplayName,
		Groups:      result.Data.Groups,
		ClientID:    result.Data.ClientID,
	}, result.Data.Resource, s.cfg.OAuth.AccessTokenTTL)
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
		ExpiresIn:    int64(s.cfg.OAuth.AccessTokenTTL.Seconds()),
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
// (default 1h via CETACEAN_OAUTH_ACCESS_TOKEN_TTL). Per RFC 7009 §2.2 the
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
		// Revoking must also drop the approval, or the next authorization
		// request is granted silently and revocation only appeared to work.
		if data, revoked := s.refreshTokens.RevokeGrant(token); revoked {
			s.consent.Forget(data.ConsentKey())
		}
	}
	// RFC 7009 §2.2: always 200 regardless of whether the token was valid.
	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// Authorize endpoint
// ---------------------------------------------------------------------------

// HandleAuthorize handles GET and POST {base}/oauth/authorize.
// consentRefusal returns the status and message for an identity that may not
// found a new authorization grant, or 0 when it may.
//
// The two refusals are different answers. No identity is 401 and carries a
// challenge, per RFC 9110 §15.5.2. A token this server issued is 403: the request
// was authenticated, and repeating it with the same credential will not help,
// which is the one thing a 401 promises.
func consentRefusal(identity *auth.Identity) (int, string) {
	switch {
	case identity == nil:
		return http.StatusUnauthorized, "authentication required"
	case identity.Provider == ProviderName:
		return http.StatusForbidden, "an access token cannot authorize a client; sign in first"
	default:
		return 0, ""
	}
}

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

// issueCodeAndRedirect mints an authorization code and redirects the browser
// back to the client with it. Both the consent-page POST and the skipped-consent
// GET path end here, so they cannot drift apart.
//
// state is separate from code because it is echoed back to the client rather
// than bound into the code.
func (s *Server) issueCodeAndRedirect(
	w http.ResponseWriter,
	r *http.Request,
	meta *ClientMetadata,
	code AuthCodeData,
	state string,
) {
	// Re-check the target against the client's registered set, even though
	// both callers already did. This is the sink for an open redirect on an
	// authorization endpoint, and hoisting the redirect into a shared helper
	// moved it away from the guard that made it safe — leaving the invariant
	// resting on a comment, and on every future caller remembering to check.
	// Keeping the guard adjacent to the redirect makes it local again.
	if !meta.HasRedirectURI(code.RedirectURI) {
		renderErrorPage(w, http.StatusBadRequest,
			"redirect_uri is not registered for this client")

		return
	}

	rawCode := s.authCodes.Issue(code, authCodeTTL)

	redirectURI, _ := url.Parse(code.RedirectURI)
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

	//nolint:gosec // G710: code.RedirectURI is exact-matched against the client's registered redirect_uris (HasRedirectURI) immediately above; this is a pre-validated URI, not open redirect.
	http.Redirect(w, r, redirectURI.String(), http.StatusFound)
}

// renderConsentPage completes a partly-built consentData with the fields only
// the server can supply — the action URL and a fresh CSRF nonce bound to this
// page's state and fingerprint — and renders the form. Both the initial GET and
// the POST that finds the client changed mid-decision go through it, so the
// second prompt is built exactly like the first.
//
// The caller sets Fingerprint to the hash of the metadata it just rendered
// from, rather than this recomputing it, so the value bound into the CSRF
// token is provably the one the caller compared against.
func (s *Server) renderConsentPage(w http.ResponseWriter, data consentData) {
	data.ActionURL = s.cfg.BasePath + "/oauth/authorize"

	// Empty unless this approval will actually be remembered, which is what
	// the template gates the disclosure on: promising to remember a client the
	// server will prompt for again is worse than saying nothing.
	if data.Verified && s.consent.Enabled() {
		data.RememberedFor = humanizeDuration(s.consent.TTL())
	}

	data.CSRFToken, _ = issueCSRFNonce(
		w,
		s.csrfKey(),
		consentBinding{
			State:         data.State,
			Fingerprint:   data.Fingerprint,
			ClientID:      data.ClientID,
			RedirectURI:   data.RedirectURI,
			CodeChallenge: data.CodeChallenge,
		},
		strings.HasPrefix(s.cfg.Issuer, "https://"),
	)

	renderConsent(w, data)
}

// Nil without a root, which hmac.New accepts: tokens would verify, forgeably,
// rather than fail.
func (s *Server) csrfKey() []byte {
	if s.keys == nil {
		return nil
	}

	return s.keys.csrf
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
	resourceAll := q["resource"]

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

	effectiveResource, err := s.resources.effectiveResource(
		resourceAll,
		s.cfg.OAuth.RequireResourceIndicator,
	)
	if err != nil {
		s.redirectWithError(w, r, redirectURIRaw, state, "invalid_target", err.Error())
		return
	}

	// Require an identity the upstream provider established.
	identity := auth.IdentityFromContext(r.Context())
	if status, refusal := consentRefusal(identity); status != 0 {
		s.renderConsentRefusal(w, status, refusal)
		return
	}

	// Fingerprint the metadata exactly once, here, from the document the page
	// is about to be rendered from. It travels to the POST in a hidden field
	// covered by the CSRF HMAC, because a record must be bound to what the
	// user was *shown*: resolving the client again on POST and fingerprinting
	// that would record a document the user may never have seen, whenever the
	// CIMD cache entry lapsed in between.
	//
	// Computed for unverified clients too. Nothing is remembered for them, but
	// the fingerprint costs a hash, and covering it uniformly means the POST
	// re-prompts whenever the name or redirect URI on screen went stale — for
	// DCR that is an LRU eviction and re-registration rather than a document
	// edit, but the user is equally owed a page describing the client that is
	// about to receive the code.
	fingerprint := consentFingerprint(meta)

	// A remembered approval skips the page. Only for verified clients, and only
	// when the metadata still hashes to what the user was shown — a CIMD client
	// controls its own document and could otherwise redirect an inherited
	// approval somewhere the user never saw.
	//
	// Issuing a code from a GET is ordinary for an authorization endpoint, and
	// redirect_uri was exact-matched against the client's registered set above,
	// so a silently issued code still lands only where the client registered.
	consentKey := ConsentKey{
		Subject:  identity.Subject,
		ClientID: clientID,
		Resource: effectiveResource,
	}

	if verified && s.consent.Allows(consentKey, fingerprint) {
		s.issueCodeAndRedirect(w, r, meta, AuthCodeData{
			ClientID:      clientID,
			RedirectURI:   redirectURIRaw,
			CodeChallenge: codeChallenge,
			Resource:      effectiveResource,
			Subject:       identity.Subject,
			Email:         identity.Email,
			DisplayName:   identity.DisplayName,
			Groups:        identity.Groups,
		}, state)

		return
	}

	s.renderConsentPage(w, consentData{
		ClientName:          meta.ClientName,
		Verified:            verified,
		RedirectURI:         redirectURIRaw,
		Subject:             identity.Subject,
		Email:               identity.Email,
		ResponseType:        responseType,
		ClientID:            clientID,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		State:               state,

		// resourceParam is the raw request parameter, echoed back unchanged:
		// the form must resubmit what the client sent, not the resolved
		// default.
		Resource:    resourceParam,
		Fingerprint: fingerprint,
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
	resourceAll := r.Form["resource"]
	decision := r.FormValue("decision")
	responseType := r.FormValue("response_type")

	// Only trustworthy once verifyCSRFToken passes: the CSRF HMAC covers this
	// field, so a token that verifies proves this is the fingerprint the
	// consent page was rendered with.
	shownFingerprint := r.FormValue(consentFingerprintField)

	// Re-validate client and redirect_uri before any redirect.
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

	secure := strings.HasPrefix(s.cfg.Issuer, "https://")

	// Validate CSRF.
	if !verifyCSRFToken(r, s.csrfKey()) {
		renderErrorPage(w, http.StatusBadRequest, "invalid or missing CSRF token")
		return
	}

	if decision == "deny" {
		clearCSRFCookie(w, secure)
		s.redirectWithError(w, r, redirectURIRaw, state, "access_denied",
			"user denied the authorization request")
		return
	}

	// Require an identity the upstream provider established.
	identity := auth.IdentityFromContext(r.Context())
	if status, refusal := consentRefusal(identity); status != 0 {
		clearCSRFCookie(w, secure)
		s.renderConsentRefusal(w, status, refusal)
		return
	}

	if responseType != "code" {
		clearCSRFCookie(w, secure)
		s.redirectWithError(w, r, redirectURIRaw, state, "unsupported_response_type",
			"response_type must be code")
		return
	}

	// PKCE is not optional, and an empty challenge must not reach a code: the
	// token endpoint would refuse every verifier against it, but a code that
	// can never be redeemed is a worse answer than a refusal here.
	if codeChallenge == "" {
		clearCSRFCookie(w, secure)
		s.redirectWithError(w, r, redirectURIRaw, state, "invalid_request",
			"code_challenge is required")
		return
	}

	if codeChallengeMethod != "S256" {
		clearCSRFCookie(w, secure)
		s.redirectWithError(w, r, redirectURIRaw, state, "invalid_request",
			"code_challenge_method must be S256")
		return
	}

	effectiveResource, err := s.resources.effectiveResource(
		resourceAll,
		s.cfg.OAuth.RequireResourceIndicator,
	)
	if err != nil {
		clearCSRFCookie(w, secure)
		s.redirectWithError(w, r, redirectURIRaw, state, "invalid_target", err.Error())
		return
	}

	// The client's metadata changed between rendering the page and this
	// submission — a CIMD document edited, or its cache entry lapsed and the
	// re-fetch returned something else. The user approved a name and a set of
	// redirect URIs that no longer describe this client, so their approval
	// does not cover this request: prompt again from the fresh metadata rather
	// than issue a code or record anything against a document they never saw.
	//
	// The cookie is deliberately not cleared here; renderConsentPage replaces
	// it with a nonce bound to the new fingerprint.
	if fingerprint := consentFingerprint(meta); fingerprint != shownFingerprint {
		s.renderConsentPage(w, consentData{
			ClientName:          meta.ClientName,
			Verified:            verified,
			RedirectURI:         redirectURIRaw,
			Subject:             identity.Subject,
			Email:               identity.Email,
			ResponseType:        responseType,
			ClientID:            clientID,
			CodeChallenge:       codeChallenge,
			CodeChallengeMethod: codeChallengeMethod,
			State:               state,
			Resource:            resourceParam,
			Fingerprint:         fingerprint,
		})

		return
	}

	// Clear the CSRF cookie — the flow is complete.
	clearCSRFCookie(w, secure)

	// Remembering is limited to verified clients. A DCR client's metadata is
	// self-reported and its client_id does not survive a restart, so a record
	// keyed on one would be worthless at best.
	//
	// The recorded fingerprint is the one the page displayed, proven current
	// by the comparison above — not a fresh resolution, which could differ
	// from what the user actually approved.
	if verified {
		s.consent.Remember(ConsentKey{
			Subject:  identity.Subject,
			ClientID: clientID,
			Resource: effectiveResource,
		}, shownFingerprint)
	}

	s.issueCodeAndRedirect(w, r, meta, AuthCodeData{
		ClientID:      clientID,
		RedirectURI:   redirectURIRaw,
		CodeChallenge: codeChallenge,
		Resource:      effectiveResource,
		Subject:       identity.Subject,
		Email:         identity.Email,
		DisplayName:   identity.DisplayName,
		Groups:        identity.Groups,
	}, state)
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
		if !s.cfg.OAuth.CIMDEnabled {
			return nil, false, cimdDisabledMessage
		}

		// Don't surface the raw fetcher error to the browser —
		// it can include DNS lookups, SSRF block reasons, "connection refused",
		// etc. Log the specifics for operators and show a generic message.
		m, err := s.cimd.Fetch(r.Context(), clientID)
		if err != nil {
			slog.Warn("CIMD fetch failed",
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

// renderConsentRefusal answers a consent request that carried the wrong kind of
// credential. RFC 9110 §15.5.2 requires a challenge on every 401, so a 401 here
// names where a usable credential comes from; a 403 is already authenticated and
// takes none.
func (s *Server) renderConsentRefusal(w http.ResponseWriter, status int, message string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", s.UnauthorizedHeader(s.resources.fallback, ""))
	}

	renderErrorPage(w, status, message)
}

// UnauthorizedHeader is the WWW-Authenticate value for a 401 from resource,
// naming that resource's own metadata document rather than a neighbouring one
// whose token this resource would also reject. An empty errorCode omits the
// error parameter per RFC 6750 §3.1; an unknown resource falls back to the
// default.
func (s *Server) UnauthorizedHeader(resource, errorCode string) string {
	target := s.resources.resourceFor(resource)

	challenge := fmt.Sprintf(
		`Bearer realm=%s, resource_metadata=%s`,
		httpQuotedString(target.Realm),
		httpQuotedString(s.cfg.metadataURL(target)),
	)
	if errorCode == "" {
		return challenge
	}

	return challenge + `, error=` + httpQuotedString(errorCode)
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
