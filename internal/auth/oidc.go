package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const maxSessionTTL = 8 * time.Hour

// OIDCProviderConfig holds the configuration for an OIDC authentication provider.
type OIDCProviderConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	SessionKey   string // hex-encoded 32-byte key; random if empty
}

// OIDCProvider implements Provider using OpenID Connect authorization code flow.
type OIDCProvider struct {
	oauth2Config          oauth2.Config
	verifier              *oidc.IDTokenVerifier
	session               *SessionCodec
	issuer                string // for RFC 9207 iss validation
	issRequired           bool   // true if IdP advertises authorization_response_iss_parameter_supported
	endSessionEndpoint    string // RFC 9722 RP-initiated logout; empty if not supported
	postLogoutRedirectURL string // derived from RedirectURL origin
}

// NewOIDCProvider creates an OIDCProvider by performing OIDC discovery on the issuer.
func NewOIDCProvider(ctx context.Context, cfg OIDCProviderConfig) (*OIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       cfg.Scopes,
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	// Extract additional discovery claims for RFC 9207 and RFC 9722.
	var disco struct {
		IssSupported       bool   `json:"authorization_response_iss_parameter_supported"`
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&disco); err != nil {
		return nil, fmt.Errorf("oidc discovery claims: %w", err)
	}

	// Derive post-logout redirect URL from the configured redirect URL origin.
	var postLogoutRedirectURL string
	if disco.EndSessionEndpoint != "" {
		if u, err := url.Parse(cfg.RedirectURL); err == nil {
			postLogoutRedirectURL = u.Scheme + "://" + u.Host + "/"
		}
	}

	var session *SessionCodec
	if cfg.SessionKey != "" {
		session, err = NewSessionCodecWithKey(cfg.SessionKey)
		if err != nil {
			return nil, fmt.Errorf("oidc session key: %w", err)
		}
	} else {
		session = NewSessionCodec()
	}

	return &OIDCProvider{
		oauth2Config:          oauth2Cfg,
		verifier:              verifier,
		session:               session,
		issuer:                cfg.Issuer,
		issRequired:           disco.IssSupported,
		endSessionEndpoint:    disco.EndSessionEndpoint,
		postLogoutRedirectURL: postLogoutRedirectURL,
	}, nil
}

// Authenticate checks for a valid session cookie or Bearer token.
// For unauthenticated browser requests it redirects to the OIDC authorize
// endpoint and returns (nil, nil). For API requests it returns an error.
func (p *OIDCProvider) Authenticate(w http.ResponseWriter, r *http.Request) (*Identity, error) {
	// 1. Check existing session cookie.
	if id, err := p.session.Get(r); err == nil {
		return id, nil
	}

	// 2. Check Bearer token.
	if token := extractBearerToken(r); token != "" {
		idToken, err := p.verifier.Verify(r.Context(), token)
		if err != nil {
			return nil, &AuthError{
				Msg:             fmt.Sprintf("invalid bearer token: %v", err),
				WWWAuthenticate: `Bearer error="invalid_token"`,
			}
		}

		var claims map[string]any
		if err := idToken.Claims(&claims); err != nil {
			return nil, fmt.Errorf("failed to parse token claims: %w", err)
		}

		if err := p.validateAzp(idToken, claims); err != nil {
			return nil, &AuthError{
				Msg:             fmt.Sprintf("invalid bearer token: %v", err),
				WWWAuthenticate: `Bearer error="invalid_token"`,
			}
		}

		return claimsToIdentity(claims), nil
	}

	// 3. Browser request: redirect to OIDC authorize endpoint.
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		redirect := r.URL.RequestURI()
		if !isRelativePath(redirect) {
			redirect = "/"
		}
		p.redirectToLogin(w, r, redirect)
		return nil, nil
	}

	// 4. API request: no credentials.
	return nil, &AuthError{
		Msg:             "authentication required",
		WWWAuthenticate: "Bearer",
	}
}

// RegisterRoutes registers the OIDC auth routes on the given mux.
// The logout endpoint uses http.CrossOriginProtection (Go 1.25+) to prevent
// cross-site logout attacks. It checks Sec-Fetch-Site first, then falls back
// to Origin-vs-Host comparison, and allows non-browser clients through.
func (p *OIDCProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", p.handleLogin)
	mux.HandleFunc("GET /auth/callback", p.handleCallback)
	mux.Handle("POST /auth/logout", http.NewCrossOriginProtection().Handler(http.HandlerFunc(p.handleLogout)))
}

func (p *OIDCProvider) handleLogin(w http.ResponseWriter, r *http.Request) {
	redirect := r.URL.Query().Get("redirect")
	if !isRelativePath(redirect) {
		redirect = "/"
	}
	p.redirectToLogin(w, r, redirect)
}

// redirectToLogin sets CSRF state, nonce, PKCE verifier, and redirect cookies,
// then redirects to the OIDC authorization endpoint. Shared by Authenticate
// (browser redirect) and handleLogin (explicit login route).
func (p *OIDCProvider) redirectToLogin(w http.ResponseWriter, r *http.Request, redirect string) {
	state := generateState()
	nonce := generateState() // same entropy, different purpose
	verifier := oauth2.GenerateVerifier()

	http.SetCookie(w, &http.Cookie{
		Name:     "cetacean_auth_state",
		Value:    state,
		Path:     "/auth",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "cetacean_auth_nonce",
		Value:    nonce,
		Path:     "/auth",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "cetacean_auth_verifier",
		Value:    verifier,
		Path:     "/auth",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "cetacean_auth_redirect",
		Value:    redirect,
		Path:     "/auth",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, p.oauth2Config.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	), http.StatusFound)
}

func (p *OIDCProvider) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Clear auth flow cookies immediately, before any early return can bypass
	// them. Set-Cookie deletion headers are written before WriteHeader, so
	// they're included in every response — both error and success paths.
	// This prevents PKCE verifiers and state values from lingering in the
	// browser after a failed callback.
	clearAuthFlowCookies(w)

	// Check for IdP-returned errors (e.g. access_denied).
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		desc := r.URL.Query().Get("error_description")
		slog.Warn("oidc authorization error", "error", errCode, "description", desc)
		http.Error(w, "authorization denied", http.StatusForbidden)
		return
	}

	// RFC 9207: validate iss parameter for mix-up attack protection.
	// If the IdP advertises authorization_response_iss_parameter_supported,
	// the iss parameter MUST be present. Always validate it when present.
	iss := r.URL.Query().Get("iss")
	if iss == "" && p.issRequired {
		slog.Error("oidc callback missing required iss parameter (IdP advertises authorization_response_iss_parameter_supported)")
		http.Error(w, "missing iss parameter", http.StatusBadRequest)
		return
	}
	if iss != "" {
		if subtle.ConstantTimeCompare([]byte(iss), []byte(p.issuer)) != 1 {
			slog.Error("oidc issuer mismatch in callback", "expected", p.issuer, "got", iss)
			http.Error(w, "issuer mismatch", http.StatusBadRequest)
			return
		}
	}

	// Validate state (constant-time comparison).
	stateCookie, err := r.Cookie("cetacean_auth_state")
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}

	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(stateCookie.Value)) != 1 {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	// Read nonce cookie for ID token verification.
	nonceCookie, err := r.Cookie("cetacean_auth_nonce")
	if err != nil {
		http.Error(w, "missing nonce cookie", http.StatusBadRequest)
		return
	}

	// Read PKCE verifier cookie.
	verifierCookie, err := r.Cookie("cetacean_auth_verifier")
	if err != nil {
		http.Error(w, "missing verifier cookie", http.StatusBadRequest)
		return
	}

	// Read redirect URL before clearing cookies.
	redirectURL := "/"
	if c, err := r.Cookie("cetacean_auth_redirect"); err == nil && isRelativePath(c.Value) {
		redirectURL = c.Value
	}

	// Exchange code for token with PKCE verifier.
	oauth2Token, err := p.oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(verifierCookie.Value))
	if err != nil {
		slog.Error("oidc token exchange failed", "error", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		slog.Error("oidc response missing id_token")
		http.Error(w, "missing id_token", http.StatusInternalServerError)
		return
	}

	idToken, err := p.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		slog.Error("oidc id_token verification failed", "error", err)
		http.Error(w, "invalid id_token", http.StatusInternalServerError)
		return
	}

	// Verify nonce matches what we sent in the authorization request.
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonceCookie.Value)) != 1 {
		slog.Error("oidc nonce mismatch")
		http.Error(w, "nonce mismatch", http.StatusBadRequest)
		return
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		slog.Error("oidc claims parsing failed", "error", err)
		http.Error(w, "failed to parse claims", http.StatusInternalServerError)
		return
	}

	if err := p.validateAzp(idToken, claims); err != nil {
		slog.Error("oidc azp validation failed", "error", err)
		http.Error(w, "azp validation failed", http.StatusBadRequest)
		return
	}

	identity := claimsToIdentity(claims)

	// Session TTL: use ID token expiry (authentication validity) capped at maxSessionTTL.
	// ID token exp is more appropriate than access token exp, which represents
	// authorization scope lifetime rather than authentication session validity.
	ttl := maxSessionTTL
	if !idToken.Expiry.IsZero() {
		if remaining := time.Until(idToken.Expiry); remaining > 0 && remaining < ttl {
			ttl = remaining
		}
	}

	// Store the raw ID token for RP-initiated logout (RFC 9722 id_token_hint).
	p.session.Set(w, identity, ttl, rawIDToken)

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// clearAuthFlowCookies deletes all temporary cookies used during the OIDC
// authorization code flow.
func clearAuthFlowCookies(w http.ResponseWriter) {
	for _, name := range []string{
		"cetacean_auth_state",
		"cetacean_auth_nonce",
		"cetacean_auth_verifier",
		"cetacean_auth_redirect",
	} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Path:     "/auth",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// authenticateQuiet checks session cookie and Bearer token without triggering
// OIDC redirects. Returns the identity or an error.
func (p *OIDCProvider) authenticateQuiet(r *http.Request) (*Identity, error) {
	if id, err := p.session.Get(r); err == nil {
		return id, nil
	}

	if token := extractBearerToken(r); token != "" {
		idToken, err := p.verifier.Verify(r.Context(), token)
		if err != nil {
			return nil, fmt.Errorf("invalid bearer token: %w", err)
		}
		var claims map[string]any
		if err := idToken.Claims(&claims); err != nil {
			return nil, fmt.Errorf("failed to parse claims: %w", err)
		}
		if err := p.validateAzp(idToken, claims); err != nil {
			return nil, fmt.Errorf("invalid bearer token: %w", err)
		}
		return claimsToIdentity(claims), nil
	}

	return nil, fmt.Errorf("authentication required")
}

func (p *OIDCProvider) handleWhoami(w http.ResponseWriter, r *http.Request) {
	id, err := p.authenticateQuiet(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(id)
}

// handleLogout clears the local session and, if the IdP supports it,
// redirects to the IdP's end_session_endpoint per RFC 9722.
func (p *OIDCProvider) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Read the ID token hint before clearing the session.
	var idTokenHint string
	if env, err := p.session.GetEnvelope(r); err == nil {
		idTokenHint = env.IDTokenHint
	}

	p.session.Clear(w)

	// If the IdP advertises an end_session_endpoint, redirect there.
	if p.endSessionEndpoint != "" {
		q := url.Values{
			"client_id": {p.oauth2Config.ClientID},
		}
		if idTokenHint != "" {
			q.Set("id_token_hint", idTokenHint)
		}
		if p.postLogoutRedirectURL != "" {
			q.Set("post_logout_redirect_uri", p.postLogoutRedirectURL)
		}
		http.Redirect(w, r, p.endSessionEndpoint+"?"+q.Encode(), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// validateAzp enforces OIDC Core Section 3.1.3.7 rules 4-5: if the ID Token
// contains multiple audiences, the azp (authorized party) claim MUST be present
// and MUST equal the client_id.
func (p *OIDCProvider) validateAzp(idToken *oidc.IDToken, claims map[string]any) error {
	if len(idToken.Audience) <= 1 {
		return nil
	}
	azp, _ := claims["azp"].(string)
	if azp == "" {
		return fmt.Errorf("multi-audience token missing required azp claim")
	}
	if azp != p.oauth2Config.ClientID {
		return fmt.Errorf("azp claim %q does not match client_id %q", azp, p.oauth2Config.ClientID)
	}
	return nil
}

// claimsToIdentity extracts identity fields from an OIDC claims map.
func claimsToIdentity(claims map[string]any) *Identity {
	id := &Identity{
		Provider: "oidc",
		Raw:      claims,
	}

	if sub, ok := claims["sub"].(string); ok {
		id.Subject = sub
	}
	if name, ok := claims["name"].(string); ok {
		id.DisplayName = name
	}
	if email, ok := claims["email"].(string); ok {
		id.Email = email
	}

	if groups, ok := claims["groups"].([]any); ok {
		for _, g := range groups {
			if s, ok := g.(string); ok {
				id.Groups = append(id.Groups, s)
			}
		}
	}

	return id
}

// extractBearerToken extracts the token from an Authorization: Bearer header.
// The scheme comparison is case-insensitive per RFC 6750 Section 2.1.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return auth[7:]
	}
	return ""
}

// isRelativePath returns true if s is a non-empty relative path (starts with /).
// Rejects absolute URLs, protocol-relative URLs, backslash sequences (some
// browsers treat \ as /), and empty strings.
func isRelativePath(s string) bool {
	if len(s) == 0 || s[0] != '/' {
		return false
	}
	// Reject protocol-relative URLs (//host) and backslash variants (/\host)
	// that some browsers normalize to //host.
	if len(s) > 1 && (s[1] == '/' || s[1] == '\\') {
		return false
	}
	if strings.Contains(s, "\\") {
		return false
	}
	return true
}

// generateState returns 16 random bytes hex-encoded for CSRF protection.
func generateState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("auth: failed to generate state: " + err.Error())
	}
	return hex.EncodeToString(b)
}
