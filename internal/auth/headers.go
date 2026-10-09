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

// maxSubjectLen caps the subject header value, so an over-long one cannot
// bloat logs, sessions or storage.
const maxSubjectLen = 256

// HeadersProvider authenticates requests using trusted proxy headers.
type HeadersProvider struct {
	cfg config.HeadersConfig

	// extraHeaders are header names whose values are captured into
	// Identity.Raw, which is how the ACL grants header reaches its source.
	extraHeaders []string
}

// NewHeadersProvider creates a HeadersProvider. Extra header names are
// captured into Identity.Raw for grant sources.
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

	if p.cfg.SecretHeader != "" {
		secret, err := singleHeader(r, p.cfg.SecretHeader)
		if err != nil {
			return nil, err
		}
		if !hmac.Equal([]byte(secret), []byte(p.cfg.SecretValue)) {
			return nil, errors.New("invalid proxy secret")
		}
	}

	subject, err := singleHeader(r, p.cfg.Subject)
	if err != nil {
		return nil, err
	}
	if err := validateSubject(subject); err != nil {
		return nil, fmt.Errorf("invalid subject header %q: %w", p.cfg.Subject, err)
	}

	displayName := subject
	if p.cfg.Name != "" {
		v, err := singleHeader(r, p.cfg.Name)
		if err != nil {
			return nil, err
		}
		if v != "" {
			displayName = v
		}
	}

	var email string
	if p.cfg.Email != "" {
		if email, err = singleHeader(r, p.cfg.Email); err != nil {
			return nil, err
		}
	}

	var groups []string
	if p.cfg.Groups != "" {
		v, err := singleHeader(r, p.cfg.Groups)
		if err != nil {
			return nil, err
		}
		for g := range strings.SplitSeq(v, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				groups = append(groups, g)
			}
		}
		if len(groups) > maxGroups {
			return nil, fmt.Errorf(
				"groups header %q names more than %d groups",
				p.cfg.Groups,
				maxGroups,
			)
		}
	}

	raw := map[string]any{
		"subject_header": p.cfg.Subject,
	}
	for _, h := range p.extraHeaders {
		v, err := singleHeader(r, h)
		if err != nil {
			return nil, err
		}
		if v != "" {
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

// maxGroups bounds the groups one request may claim; every grant's audience
// is matched against each of them.
const maxGroups = 256

// singleHeader reads a header the proxy must set exactly once. A proxy that
// appends rather than replaces leaves its client's value beside its own, so a
// second value means one of them came from the client.
func singleHeader(r *http.Request, name string) (string, error) {
	values := r.Header.Values(name)
	if len(values) > 1 {
		return "", fmt.Errorf(
			"header %q appears more than once; the proxy must replace any its client sent",
			name,
		)
	}
	if len(values) == 0 {
		return "", nil
	}

	return values[0], nil
}

// validateSubject rejects an empty, over-long, or control-character subject.
// No error quotes the value: WhoamiHandler logs the error this is wrapped
// into, so anything echoed here puts a proxy-supplied header in the log.
func validateSubject(s string) error {
	if s == "" {
		return errors.New("empty value")
	}

	if len(s) > maxSubjectLen {
		return fmt.Errorf("exceeds maximum length (%d > %d)", len(s), maxSubjectLen)
	}

	for _, r := range s {
		if unicode.IsControl(r) {
			return errors.New("contains a control character")
		}
	}

	return nil
}
