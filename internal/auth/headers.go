package auth

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/radiergummi/cetacean/internal/config"
)

// HeadersProvider authenticates requests using trusted proxy headers.
type HeadersProvider struct {
	cfg config.HeadersConfig
}

// NewHeadersProvider creates a new HeadersProvider with the given configuration.
func NewHeadersProvider(cfg config.HeadersConfig) *HeadersProvider {
	return &HeadersProvider{cfg: cfg}
}

// Authenticate reads identity information from request headers set by a
// trusted reverse proxy. If SecretHeader is configured, the proxy must also
// send a matching secret value.
func (p *HeadersProvider) Authenticate(_ http.ResponseWriter, r *http.Request) (*Identity, error) {
	if p.cfg.SecretHeader != "" {
		got := r.Header.Get(p.cfg.SecretHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(p.cfg.SecretValue)) != 1 {
			return nil, errors.New("invalid proxy secret")
		}
	}

	subject := r.Header.Get(p.cfg.Subject)
	if subject == "" {
		return nil, fmt.Errorf("missing subject header %q", p.cfg.Subject)
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
			"subject":        subject,
		},
	}, nil
}

func (p *HeadersProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", WhoamiHandler(p))
}
