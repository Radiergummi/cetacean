package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
}

// OIDCProvider implements Provider using OpenID Connect authorization code flow.
type OIDCProvider struct {
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	session      *SessionCodec
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

	return &OIDCProvider{
		oauth2Config: oauth2Cfg,
		verifier:     verifier,
		session:      NewSessionCodec(),
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
			return nil, fmt.Errorf("invalid bearer token: %w", err)
		}

		var claims map[string]any
		if err := idToken.Claims(&claims); err != nil {
			return nil, fmt.Errorf("failed to parse token claims: %w", err)
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

	// 4. API request: return error.
	return nil, errors.New("authentication required")
}

// RegisterRoutes registers the OIDC auth routes on the given mux.
func (p *OIDCProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", p.handleLogin)
	mux.HandleFunc("GET /auth/callback", p.handleCallback)
	mux.HandleFunc("GET /auth/logout", p.handleLogout)
	mux.HandleFunc("GET /auth/whoami", p.handleWhoami)
}

func (p *OIDCProvider) handleLogin(w http.ResponseWriter, r *http.Request) {
	redirect := r.URL.Query().Get("redirect")
	if !isRelativePath(redirect) {
		redirect = "/"
	}
	p.redirectToLogin(w, r, redirect)
}

// redirectToLogin sets CSRF state and redirect cookies, then redirects to the
// OIDC authorization endpoint. Shared by Authenticate (browser redirect) and
// handleLogin (explicit login route).
func (p *OIDCProvider) redirectToLogin(w http.ResponseWriter, r *http.Request, redirect string) {
	state := generateState()

	http.SetCookie(w, &http.Cookie{
		Name:     "cetacean_auth_state",
		Value:    state,
		Path:     "/auth",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "cetacean_auth_redirect",
		Value:    redirect,
		Path:     "/auth",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, p.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

func (p *OIDCProvider) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state.
	stateCookie, err := r.Cookie("cetacean_auth_state")
	if err != nil {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}

	if r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	// Read redirect URL before clearing cookies.
	redirectURL := "/"
	if c, err := r.Cookie("cetacean_auth_redirect"); err == nil && isRelativePath(c.Value) {
		redirectURL = c.Value
	}

	// Exchange code for token.
	oauth2Token, err := p.oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"))
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

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		slog.Error("oidc claims parsing failed", "error", err)
		http.Error(w, "failed to parse claims", http.StatusInternalServerError)
		return
	}

	identity := claimsToIdentity(claims)

	// Session TTL: use token expiry capped at maxSessionTTL.
	ttl := maxSessionTTL
	if !oauth2Token.Expiry.IsZero() {
		if remaining := time.Until(oauth2Token.Expiry); remaining > 0 && remaining < ttl {
			ttl = remaining
		}
	}

	p.session.Set(w, identity, ttl)

	// Clear state cookies.
	http.SetCookie(w, &http.Cookie{Name: "cetacean_auth_state", Path: "/auth", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "cetacean_auth_redirect", Path: "/auth", MaxAge: -1})

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// handleWhoami returns the current identity from session cookie or Bearer
// token, without triggering OIDC redirects. Returns 401 if unauthenticated.
func (p *OIDCProvider) handleWhoami(w http.ResponseWriter, r *http.Request) {
	// Check session cookie.
	if id, err := p.session.Get(r); err == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(id)
		return
	}

	// Check Bearer token.
	if token := extractBearerToken(r); token != "" {
		idToken, err := p.verifier.Verify(r.Context(), token)
		if err == nil {
			var claims map[string]any
			if err := idToken.Claims(&claims); err == nil {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(claimsToIdentity(claims))
				return
			}
		}
	}

	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func (p *OIDCProvider) handleLogout(w http.ResponseWriter, r *http.Request) {
	p.session.Clear(w)
	http.Redirect(w, r, "/", http.StatusFound)
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
// Rejects absolute URLs, protocol-relative URLs, and empty strings.
func isRelativePath(s string) bool {
	return len(s) > 0 && s[0] == '/' && (len(s) == 1 || s[1] != '/')
}

// generateState returns 16 random bytes hex-encoded for CSRF protection.
func generateState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("auth: failed to generate state: " + err.Error())
	}
	return hex.EncodeToString(b)
}
