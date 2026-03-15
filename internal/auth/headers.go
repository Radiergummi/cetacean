package auth

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"unicode"

	"github.com/radiergummi/cetacean/internal/config"
)

// maxSubjectLen caps the subject header value to prevent abuse via
// extremely long headers that could bloat logs, sessions, or storage.
const maxSubjectLen = 256

// HeadersProvider authenticates requests using trusted proxy headers.
type HeadersProvider struct {
	cfg config.HeadersConfig
}

// NewHeadersProvider creates a new HeadersProvider with the given configuration.
func NewHeadersProvider(cfg config.HeadersConfig) *HeadersProvider {
	return &HeadersProvider{cfg: cfg}
}

// Authenticate reads identity information from request headers set by a
// trusted reverse proxy. If TrustedProxies is configured, the request's
// remote address must match. If SecretHeader is configured, the proxy must
// also send a matching secret value.
func (p *HeadersProvider) Authenticate(_ http.ResponseWriter, r *http.Request) (*Identity, error) {
	// Check trusted proxy allowlist.
	if len(p.cfg.TrustedProxies) > 0 {
		if err := p.validateSourceIP(r.RemoteAddr); err != nil {
			return nil, err
		}
	}

	// Check shared secret (constant-time).
	if p.cfg.SecretHeader != "" {
		got := []byte(r.Header.Get(p.cfg.SecretHeader))
		want := []byte(p.cfg.SecretValue)
		if !hmac.Equal(got, want) {
			return nil, errors.New("invalid proxy secret")
		}
	}

	subject := r.Header.Get(p.cfg.Subject)
	if err := validateSubject(subject); err != nil {
		return nil, fmt.Errorf("invalid subject header %q: %w", p.cfg.Subject, err)
	}

	displayName := subject
	if p.cfg.Name != "" {
		if v := r.Header.Get(p.cfg.Name); v != "" {
			displayName = v
		}
	}

	var email string
	if p.cfg.Email != "" {
		email = r.Header.Get(p.cfg.Email)
	}

	var groups []string
	if p.cfg.Groups != "" {
		if v := r.Header.Get(p.cfg.Groups); v != "" {
			for _, g := range strings.Split(v, ",") {
				g = strings.TrimSpace(g)
				if g != "" {
					groups = append(groups, g)
				}
			}
		}
	}

	return &Identity{
		Subject:     subject,
		Provider:    "headers",
		DisplayName: displayName,
		Email:       email,
		Groups:      groups,
		Raw: map[string]any{
			"subject_header": p.cfg.Subject,
		},
	}, nil
}

func (p *HeadersProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", WhoamiHandler(p))
}

// validateSourceIP checks that the request originates from a trusted proxy.
func (p *HeadersProvider) validateSourceIP(remoteAddr string) error {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return errors.New("invalid remote address")
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return errors.New("invalid remote IP")
	}

	for _, prefix := range p.cfg.TrustedProxies {
		if prefix.Contains(addr) {
			return nil
		}
	}

	return fmt.Errorf("remote address %s is not a trusted proxy", addr)
}

// validateSubject checks the subject header value for sanity: non-empty,
// no control characters, and within length limits.
func validateSubject(s string) error {
	if s == "" {
		return errors.New("empty value")
	}

	if len(s) > maxSubjectLen {
		return fmt.Errorf("exceeds maximum length (%d > %d)", len(s), maxSubjectLen)
	}

	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("contains control character U+%04X", r)
		}
	}

	return nil
}
