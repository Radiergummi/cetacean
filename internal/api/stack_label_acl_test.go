package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestPatchLabels_StackNamespaceNeedsWriteOnTheDestinationStack(t *testing.T) {
	const ns = "com.docker.stack.namespace"

	tests := []struct {
		name   string
		target string
		want   int
	}{
		{"into another tenant's stack", "globex", http.StatusForbidden},
		{"into a writable stack", "acme-staging", http.StatusOK},
	}

	for _, tt := range tests {
		for _, kind := range []string{"service", "config", "secret"} {
			t.Run(kind+" "+tt.name, func(t *testing.T) {
				labels := map[string]string{ns: "acme"}
				c := cache.New(nil)
				c.SetService(swarm.Service{ID: "r1", Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{Name: "acme_web", Labels: labels},
				}})
				c.SetConfig(swarm.Config{ID: "r1", Spec: swarm.ConfigSpec{
					Annotations: swarm.Annotations{Name: "acme_cfg", Labels: labels},
				}})
				c.SetSecret(swarm.Secret{ID: "r1", Spec: swarm.SecretSpec{
					Annotations: swarm.Annotations{Name: "acme_sec", Labels: labels},
				}})

				e := acl.NewEvaluator()
				e.SetResolver(c)
				e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
					Resources:   []string{"stack:acme", "stack:acme-staging"},
					Audience:    []string{"user:acme"},
					Permissions: []string{"write"},
				}}})

				written := false
				wc := &mockWriteClient{}
				wc.simulatedLabels = map[string]string{ns: "acme"}
				wc.simulatedConfigLabels = map[string]string{ns: "acme"}
				wc.simulatedSecretLabels = map[string]string{ns: "acme"}
				wc.updateServiceLabelsFn = func(
					_ context.Context, id string, l map[string]string,
				) (swarm.Service, error) {
					written = true
					return swarm.Service{ID: id}, nil
				}
				wc.updateConfigLabelsFn = func(
					_ context.Context, id string, l map[string]string,
				) (swarm.Config, error) {
					written = true
					return swarm.Config{ID: id}, nil
				}
				wc.updateSecretLabelsFn = func(
					_ context.Context, id string, l map[string]string,
				) (swarm.Secret, error) {
					written = true
					return swarm.Secret{ID: id}, nil
				}

				h := newTestHandlers(t, withCache(c), withACL(e), withWriteClient(wc),
					withOpsLevel(config.OpsConfiguration))

				body := `{"` + ns + `":"` + tt.target + `"}`
				req := httptest.NewRequest("PATCH", "/"+kind+"s/r1/labels", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/merge-patch+json")
				req.SetPathValue("id", "r1")
				req = req.WithContext(
					auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "acme"}),
				)
				w := httptest.NewRecorder()

				switch kind {
				case "service":
					h.HandlePatchServiceLabels(w, req)
				case "config":
					h.HandlePatchConfigLabels(w, req)
				case "secret":
					h.HandlePatchSecretLabels(w, req)
				}

				if w.Code != tt.want {
					t.Fatalf("status=%d, want %d; body: %s", w.Code, tt.want, w.Body.String())
				}
				if tt.want == http.StatusForbidden {
					assertACLErrorCode(t, w, "ACL002")
					if written {
						t.Fatal("labels were written despite the denial")
					}
				}
			})
		}
	}
}
