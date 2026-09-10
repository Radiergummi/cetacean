package auth

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"net/http"
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

	// extraHeaders are additional header names whose values are captured
	// into Identity.Raw. Used to pass the ACL grants header through to
	// the grant source.
	extraHeaders []string
}

// NewHeadersProvider creates a new HeadersProvider with the given configuration.
// Extra header names are captured into Identity.Raw for use by grant sources.
func NewHeadersProvider(cfg config.HeadersConfig, extraHeaders ...string) *HeadersProvider {
	return &HeadersProvider{cfg: cfg, extraHeaders: extraHeaders}
}

// Authenticate reads identity information from request headers set by a
// trusted reverse proxy, which the edge must have vouched for. If
// SecretHeader is configured, the proxy must also send a matching secret.
func (p *HeadersProvider) Authenticate(_ http.ResponseWriter, r *http.Request) (*Identity, error) {
	// Not re-derived from RemoteAddr: realIP has by then rewritten it to the
	// client address the proxy reported, so the check would ask whether the
	// *client* is a trusted proxy.
	if !FromTrustedProxy(r.Context()) {
		return nil, errors.New("request did not arrive through a trusted proxy")
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
			for g := range strings.SplitSeq(v, ",") {
				g = strings.TrimSpace(g)
				if g != "" {
					groups = append(groups, g)
				}
			}
		}
	}

	raw := map[string]any{
		"subject_header": p.cfg.Subject,
	}
	for _, h := range p.extraHeaders {
		if v := r.Header.Get(h); v != "" {
			raw[h] = v
		}
	}

	return &Identity{
		Subject:     subject,
		Provider:    "headers",
		DisplayName: displayName,
		Email:       email,
		Groups:      groups,
		Raw:         raw,
	}, nil
}

func (p *HeadersProvider) RegisterRoutes(_ *http.ServeMux) {}

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
