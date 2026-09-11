package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// canonicalTestCache holds one service, one config and two nodes sharing a
// hostname — Swarm does not require node hostnames to be unique, which is the
// only way an identifier can be genuinely ambiguous.
func canonicalTestCache() *cache.Cache {
	c := cache.New(nil)

	c.SetService(swarm.Service{
		ID:   "svc1234567890",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "shop_web"}},
	})

	c.SetConfig(swarm.Config{
		ID:   "cfg1234567890",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "shop_nginx"}},
	})

	for _, id := range []string{"nodeaaaaaaaaaa", "nodebbbbbbbbbb"} {
		c.SetNode(swarm.Node{
			ID:          id,
			Description: swarm.NodeDescription{Hostname: "twin"},
		})
	}

	return c
}

func TestCanonicalIdentifierRedirectsNameToID(t *testing.T) {
	router := newTestRouterWithCache(t, canonicalTestCache())

	cases := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{"service detail", http.MethodGet, "/services/shop_web", "/services/svc1234567890"},
		{
			"service sub-resource",
			http.MethodGet,
			"/services/shop_web/env",
			"/services/svc1234567890/env",
		},
		{
			"write endpoint",
			http.MethodPut,
			"/services/shop_web/scale",
			"/services/svc1234567890/scale",
		},
		{"config detail", http.MethodGet, "/configs/shop_nginx", "/configs/cfg1234567890"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Accept", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusTemporaryRedirect {
				t.Fatalf("status = %d, want 307; body: %s", w.Code, w.Body.String())
			}

			if got := w.Header().Get("Location"); got != tc.want {
				t.Errorf("Location = %q, want %q", got, tc.want)
			}
		})
	}
}

// A 307 is the only redirect that may carry a write: 301 and 302 let a client
// rewrite the method to GET, and 308 is cacheable indefinitely while a name
// can be moved to another resource.
func TestCanonicalIdentifierPreservesQueryString(t *testing.T) {
	router := newTestRouterWithCache(t, canonicalTestCache())

	req := httptest.NewRequest(http.MethodGet, "/services/shop_web/logs?tail=50&follow=true", nil)
	req.Header.Set("Accept", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	want := "/services/svc1234567890/logs?tail=50&follow=true"
	if got := w.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

// An identifier that is already canonical must reach its handler untouched;
// a redirect here would cost every well-behaved client a second round trip.
func TestCanonicalIdentifierLeavesIDsAlone(t *testing.T) {
	router := newTestRouterWithCache(t, canonicalTestCache())

	req := httptest.NewRequest(http.MethodGet, "/services/svc1234567890", nil)
	req.Header.Set("Accept", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// A name matching nothing must produce the handler's own 404, not a redirect
// to an empty path.
func TestCanonicalIdentifierUnknownNameStill404s(t *testing.T) {
	router := newTestRouterWithCache(t, canonicalTestCache())

	req := httptest.NewRequest(http.MethodGet, "/services/nope", nil)
	req.Header.Set("Accept", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// Two nodes may share a hostname, so the name identifies neither. Answering
// with either would describe the wrong machine.
func TestCanonicalIdentifierAmbiguousNameIs409(t *testing.T) {
	router := newTestRouterWithCache(t, canonicalTestCache())

	req := httptest.NewRequest(http.MethodGet, "/nodes/twin", nil)
	req.Header.Set("Accept", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	for _, id := range []string{"nodeaaaaaaaaaa", "nodebbbbbbbbbb"} {
		if !strings.Contains(body, id) {
			t.Errorf("body does not name candidate %s: %s", id, body)
		}
	}
}

// The redirect states that a named resource exists and hands over its ID, so
// it must not answer a caller who could not read the resource anyway —
// otherwise every detail path becomes a way to enumerate names behind the
// policy. Such a request falls through to the handler, which denies it exactly
// as it does for the ID.
func TestCanonicalIdentifierDoesNotRedirectWithoutReadGrant(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:other_*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	router := newTestRouterWithCache(t, canonicalTestCache(), withACL(evaluator))

	req := httptest.NewRequest(http.MethodGet, "/services/shop_web", nil)
	req.Header.Set("Accept", "application/json")
	req = req.WithContext(
		auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}),
	)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusTemporaryRedirect {
		t.Fatalf(
			"redirected to %q for an identity with no read grant",
			w.Header().Get("Location"),
		)
	}

	if got := w.Header().Get("Location"); got != "" {
		t.Errorf("Location = %q, want it unset", got)
	}
}

// A caller that can read the resource gets the redirect, so the test above
// cannot pass merely because the ACL path is broken for everyone.
func TestCanonicalIdentifierRedirectsWithReadGrant(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:shop_*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	router := newTestRouterWithCache(t, canonicalTestCache(), withACL(evaluator))

	req := httptest.NewRequest(http.MethodGet, "/services/shop_web", nil)
	req.Header.Set("Accept", "application/json")
	req = req.WithContext(
		auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}),
	)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307; body: %s", w.Code, w.Body.String())
	}
}

// HTML requests are the SPA's routing surface: the dashboard resolves its own
// paths in the browser, so canonicalising them here would take the choice of
// what a dashboard URL looks like away from the frontend.
func TestCanonicalIdentifierIgnoresHTML(t *testing.T) {
	router := newTestRouterWithCache(t, canonicalTestCache())

	req := httptest.NewRequest(http.MethodGet, "/services/shop_web", nil)
	req.Header.Set("Accept", "text/html")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusTemporaryRedirect {
		t.Fatalf("HTML request was redirected to %q", w.Header().Get("Location"))
	}
}

// Volumes and stacks are keyed by name, so their detail paths already carry
// the canonical identifier and must not be rewritten.
func TestCanonicalIdentifierSkipsNameKeyedTypes(t *testing.T) {
	router := newTestRouterWithCache(t, canonicalTestCache())

	for _, path := range []string{"/volumes/shop-data", "/stacks/shop"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code == http.StatusTemporaryRedirect {
				t.Errorf("%s was redirected to %q", path, w.Header().Get("Location"))
			}
		})
	}
}

func TestSplitResourcePath(t *testing.T) {
	cases := []struct {
		path       string
		collection string
		identifier string
		rest       string
	}{
		{"/services", "services", "", ""},
		{"/services/", "services", "", ""},
		{"/services/abc", "services", "abc", ""},
		{"/services/abc/env", "services", "abc", "/env"},
		{"/services/abc/logs/extra", "services", "abc", "/logs/extra"},
		{"/", "", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			collection, identifier, rest := splitResourcePath(tc.path)

			if collection != tc.collection || identifier != tc.identifier || rest != tc.rest {
				t.Errorf(
					"splitResourcePath(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.path, collection, identifier, rest,
					tc.collection, tc.identifier, tc.rest,
				)
			}
		})
	}
}

// The 409 names every candidate ID, so it discloses both the existence of the
// name and the IDs behind it. A caller who could read none of that type must be
// told nothing — they get the handler's ordinary 404 for an unresolved name.
func TestCanonicalIdentifierAmbiguityIsNotReportedWithoutAGrant(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	router := newTestRouterWithCache(t, canonicalTestCache(), withACL(evaluator))

	req := httptest.NewRequest(http.MethodGet, "/nodes/twin", nil)
	req.Header.Set("Accept", "application/json")
	req = req.WithContext(
		auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}),
	)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusConflict {
		t.Fatalf("ambiguity reported to an identity with no node grant: %s", w.Body.String())
	}

	for _, id := range []string{"nodeaaaaaaaaaa", "nodebbbbbbbbbb"} {
		if strings.Contains(w.Body.String(), id) {
			t.Errorf("response names candidate %s: %s", id, w.Body.String())
		}
	}
}

// The converse, so the test above cannot pass merely because the report is
// broken for everyone.
func TestCanonicalIdentifierAmbiguityIsReportedWithANodeGrant(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"node:*"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	router := newTestRouterWithCache(t, canonicalTestCache(), withACL(evaluator))

	req := httptest.NewRequest(http.MethodGet, "/nodes/twin", nil)
	req.Header.Set("Accept", "application/json")
	req = req.WithContext(
		auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "alice"}),
	)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}
