package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// A token is a credential its holder leaves on a device, so a deployment may
// hold it to less than the person it speaks for. The same person over a session
// keeps the deployment's own tier.
func TestATokenIsHeldToItsOwnCeiling(t *testing.T) {
	h := &Handlers{
		operationsLevel:      config.OpsImpactful,
		tokenOperationsLevel: config.OpsReadOnly,
	}

	gated := h.requireLevel(config.OpsOperational)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	for name, tc := range map[string]struct {
		identity *auth.Identity
		want     int
	}{
		"a session reaches the deployment's tier": {
			identity: &auth.Identity{Subject: "alice", Provider: "oidc"},
			want:     http.StatusNoContent,
		},
		"a token is held to its own": {
			identity: &auth.Identity{Subject: "alice", Provider: auth.ProviderToken},
			want:     http.StatusForbidden,
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/services/web/scale", nil)
			req = req.WithContext(auth.ContextWithIdentity(req.Context(), tc.identity))
			rec := httptest.NewRecorder()

			gated.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}

			if tc.want == http.StatusForbidden &&
				!strings.Contains(rec.Body.String(), "OPS001") {
				t.Errorf("refusal did not name OPS001: %s", rec.Body.String())
			}
		})
	}
}

// Without a ceiling of its own a token is the person it speaks for, which is
// what every deployment that configures nothing gets.
func TestATokenWithoutACeilingReachesTheDeploymentsTier(t *testing.T) {
	h := &Handlers{
		operationsLevel:      config.OpsOperational,
		tokenOperationsLevel: config.OpsOperational,
	}

	gated := h.requireLevel(config.OpsOperational)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/services/web/scale", nil)
	req = req.WithContext(auth.ContextWithIdentity(
		req.Context(),
		&auth.Identity{Subject: "alice", Provider: auth.ProviderToken},
	))
	rec := httptest.NewRecorder()

	gated.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}
