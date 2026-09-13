package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// composeCache is a one-service stack plus the network that service attaches
// to, which is what resolves the attachment's ID.
func composeCache(t testing.TB) *cache.Cache {
	t.Helper()

	c := cache.New(nil)
	c.SetNetwork(network.Summary{
		ID:     "netid-internal",
		Name:   "web_internal",
		Driver: "overlay",
		Labels: map[string]string{"com.docker.stack.namespace": "web"},
	})
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "web_api",
				Labels: map[string]string{"com.docker.stack.namespace": "web"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.27"},
				Networks: []swarm.NetworkAttachmentConfig{
					{Target: "netid-internal"},
				},
			},
		},
	})

	return c
}

func TestStackComposeRendersYAML(t *testing.T) {
	router := newTestRouterWithCache(t, composeCache(t))

	req := httptest.NewRequest(http.MethodGet, "/stacks/web.yaml", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("Content-Type = %q, want application/yaml", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "# Exported from Cetacean.") {
		t.Errorf("document lost the header:\n%s", body)
	}
	// The stack's own prefix comes off the key, and the attachment resolves to
	// a name rather than the ID Swarm carries.
	if !strings.Contains(body, "\n  api:") {
		t.Errorf("service key is not shortened:\n%s", body)
	}
	if strings.Contains(body, "netid-internal") {
		t.Errorf("document leaks a network ID:\n%s", body)
	}
}

// Accept must reach the same projection the suffix does, or the two ways of
// asking for a representation disagree.
func TestStackComposeAnswersAcceptAsWellAsTheSuffix(t *testing.T) {
	router := newTestRouterWithCache(t, composeCache(t))

	req := httptest.NewRequest(http.MethodGet, "/stacks/web", nil)
	req.Header.Set("Accept", "application/yaml")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "# Exported from Cetacean.") {
		t.Errorf("Accept did not reach the compose projection:\n%s", rec.Body)
	}
}

func TestServiceComposeRendersYAML(t *testing.T) {
	router := newTestRouterWithCache(t, composeCache(t))

	req := httptest.NewRequest(http.MethodGet, "/services/svc1.yaml", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	// A single service owns nothing, so its network is external and keeps the
	// full Swarm name.
	if !strings.Contains(body, "web_internal:") || !strings.Contains(body, "external: true") {
		t.Errorf("single-service export does not declare its network external:\n%s", body)
	}
}

// An endpoint that renders no compose document must keep refusing the type
// rather than falling through to JSON.
func TestComposeIsNotOfferedWhereItIsNotServed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/services.yaml", nil)
	rec := httptest.NewRecorder()
	newTestRouterWithCache(t, composeCache(t)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotAcceptable {
		t.Fatalf("status = %d, want 406 on a list endpoint", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "application/yaml") {
		t.Errorf("406 offers a type the endpoint does not serve:\n%s", rec.Body)
	}
}

// The export reads the stack through the same grant the JSON detail does, or
// it is a way around the policy.
func TestComposeExportHonoursTheSameACLAsTheJSONDetail(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"stack:other"},
		Audience:    []string{"*"},
		Permissions: []string{"read"},
	}}})

	router := newTestRouterWithCache(t, composeCache(t), withACL(e))

	for _, path := range []string{"/stacks/web.yaml", "/stacks/web"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if path == "/stacks/web" {
			req.Header.Set("Accept", "application/yaml")
		}
		req = req.WithContext(
			auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"}),
		)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			t.Errorf("%s: served a stack this identity has no grant for:\n%s", path, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "web_api") {
			t.Errorf("%s: leaked a service name:\n%s", path, rec.Body)
		}
	}
}

// Docker permits a dot in a name, so this is reachable. The suffix wins —
// consistent with .json, which has had the same property since negotiate
// existed — and the ID form is how such a service is addressed instead.
func TestServiceNamedLikeAnExtensionResolvesAsARepresentation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/services/web.yml", nil)
	rec := httptest.NewRecorder()
	newTestRouterWithCache(t, composeCache(t)).ServeHTTP(rec, req)

	// "web" is not a service ID in the cache, so the lookup refuses; what
	// matters is that the suffix was read as a representation request and not
	// as part of the identifier.
	if rec.Code == http.StatusOK {
		t.Fatalf("status = 200, want a refusal for a service that does not exist")
	}
	if strings.Contains(rec.Body.String(), "web.yml") {
		t.Errorf("the suffix was kept as part of the identifier:\n%s", rec.Body)
	}
}
