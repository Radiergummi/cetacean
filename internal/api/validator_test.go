package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func validatorCache(n int) *cache.Cache {
	c := cache.New(nil)
	for i := range n {
		c.SetNode(swarm.Node{
			ID:          fmt.Sprintf("id-%d", i),
			Description: swarm.NodeDescription{Hostname: fmt.Sprintf("node-%d", i)},
			Spec:        swarm.NodeSpec{Role: swarm.NodeRoleWorker},
			Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		})
	}
	return c
}

func listOnce(
	t *testing.T,
	h *Handlers,
	target string,
	id *auth.Identity,
	inm string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	if id != nil {
		req = req.WithContext(auth.ContextWithIdentity(req.Context(), id))
	}
	if inm != "" {
		req.Header.Set("If-None-Match", inm)
	}
	w := httptest.NewRecorder()
	h.HandleListNodes(w, req)
	return w
}

// The contract the whole scheme rests on: the tag handed out with a 200 is the
// one the conditional path recognises. If they diverge, every revalidation
// misses and the client silently refetches forever.
func TestValidatorRoundTrips(t *testing.T) {
	h := newTestHandlers(t, withCache(validatorCache(30)))

	first := listOnce(t, h, "/api/nodes", nil, "")
	if first.Code != http.StatusOK {
		t.Fatalf("first request returned %d", first.Code)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the list response")
	}

	second := listOnce(t, h, "/api/nodes", nil, etag)
	if second.Code != http.StatusNotModified {
		t.Fatalf("revalidating with the tag just issued returned %d, want 304", second.Code)
	}
	if got := second.Header().Get("ETag"); got != etag {
		t.Errorf("304 carried ETag %q, the 200 carried %q", got, etag)
	}
	if second.Body.Len() != 0 {
		t.Errorf("304 carried a %d byte body", second.Body.Len())
	}
}

// The dashboard gates its controls on Allow, so a 304 that dropped it would
// silently change what the UI offers.
func TestValidatorNotModifiedKeepsAllow(t *testing.T) {
	h := newTestHandlers(t, withCache(validatorCache(5)))

	full := listOnce(t, h, "/api/nodes", nil, "")
	etag := full.Header().Get("ETag")
	notModified := listOnce(t, h, "/api/nodes", nil, etag)

	for _, header := range []string{"Allow", "ETag", "Vary", "Cache-Control", "Accept-Ranges"} {
		if got := notModified.Header().Get(header); got == "" {
			t.Errorf("304 dropped %s, which the 200 set to %q", header, full.Header().Get(header))
		}
	}
	if got, want := notModified.Header().Get("Allow"), full.Header().Get("Allow"); got != want {
		t.Errorf("304 reported Allow %q, the 200 reported %q", got, want)
	}
}

// Every input that changes the body has to change the tag. A component that
// stops mattering shows up here rather than as one caller receiving another's
// 304.
func TestDerivedETagVaries(t *testing.T) {
	c := validatorCache(20)
	h := newTestHandlers(t, withCache(c))

	base := "/api/nodes"
	baseline := listOnce(t, h, base, nil, "").Header().Get("ETag")

	variants := map[string]string{
		"search":       base + "?search=node-1",
		"filter":       base + "?filter=" + "role+%3D%3D+%22worker%22",
		"sort":         base + "?sort=hostname",
		"direction":    base + "?sort=hostname&dir=desc",
		"page":         base + "?page=2",
		"per_page":     base + "?per_page=5",
		"another path": "/api/nodes/",
	}

	for name, target := range variants {
		t.Run(name, func(t *testing.T) {
			if got := listOnce(t, h, target, nil, "").Header().Get("ETag"); got == baseline {
				t.Errorf("%s did not change the validator; a client holding the "+
					"unqualified listing would be told its copy is current", name)
			}
		})
	}

	t.Run("cache mutation", func(t *testing.T) {
		c.SetNode(swarm.Node{ID: "fresh"})
		if got := listOnce(t, h, base, nil, "").Header().Get("ETag"); got == baseline {
			t.Error("adding a node did not change the validator")
		}
	})
}

// Two identities that see different rows must never share a tag.
func TestDerivedETagSeparatesIdentities(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"node:*"},
			Permissions: []string{"read"},
			Audience:    []string{"group:all"},
		},
		{
			Resources:   []string{"node:node-0"},
			Permissions: []string{"read"},
			Audience:    []string{"group:one"},
		},
	}})
	h := newTestHandlers(t, withCache(validatorCache(8)), withACL(e))

	broad := &auth.Identity{Subject: "b", Groups: []string{"all"}}
	narrow := &auth.Identity{Subject: "n", Groups: []string{"one"}}

	broadTag := listOnce(t, h, "/api/nodes", broad, "").Header().Get("ETag")
	narrowTag := listOnce(t, h, "/api/nodes", narrow, "").Header().Get("ETag")

	if broadTag == narrowTag {
		t.Fatal("identities with different grants share a validator")
	}

	// And the narrow identity must not be able to revalidate with the broad
	// identity's tag.
	if got := listOnce(t, h, "/api/nodes", narrow, broadTag); got.Code == http.StatusNotModified {
		t.Error("a narrowly-scoped caller revalidated successfully with another " +
			"identity's validator, and would have kept that listing")
	}
}

// A reload changes what an identity may see with no cache mutation behind it.
func TestDerivedETagFollowsPolicyReload(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"node:*"}, Permissions: []string{"read"}},
	}})
	h := newTestHandlers(t, withCache(validatorCache(8)), withACL(e))
	id := &auth.Identity{Subject: "x"}

	before := listOnce(t, h, "/api/nodes", id, "").Header().Get("ETag")

	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"node:node-0"}, Permissions: []string{"read"}},
	}})

	after := listOnce(t, h, "/api/nodes", id, "").Header().Get("ETag")
	if after == before {
		t.Error("narrowing the policy left the validator unchanged; the caller " +
			"would keep being told the wider listing is current")
	}
	if got := listOnce(t, h, "/api/nodes", id, before).Code; got == http.StatusNotModified {
		t.Error("a tag issued under the old policy still revalidates")
	}
}

// The case a history-derived generation would have missed.
func TestDerivedETagFollowsResync(t *testing.T) {
	c := validatorCache(6)
	h := newTestHandlers(t, withCache(c))

	before := listOnce(t, h, "/api/nodes", nil, "").Header().Get("ETag")

	c.ReplaceAll(cache.FullSyncData{
		HasNodes: true,
		Nodes:    []swarm.Node{{ID: "replaced"}},
	})

	if got := listOnce(t, h, "/api/nodes", nil, before).Code; got == http.StatusNotModified {
		t.Error("a full resync left the previous validator matching, so a client " +
			"keeps a listing of nodes that no longer exist")
	}
}

// RFC 9110 §13.2.1: a precondition is ignored when the unconditional response
// would not have been a 2xx. The derived validator answers before anything has
// parsed the query, so a bare "*" must not reach it — the malformed filter owes
// the client its error, not a 304.
func TestWildcardDoesNotPreemptRequestValidation(t *testing.T) {
	h := newTestHandlers(t, withCache(validatorCache(5)))

	bad := listOnce(t, h, "/api/nodes?filter=%29%29bogus", nil, "*")
	if bad.Code != http.StatusBadRequest {
		t.Errorf("a malformed filter with If-None-Match: * returned %d, want 400", bad.Code)
	}

	// A valid request still gets its 304 — from the writer, once rendered.
	good := listOnce(t, h, "/api/nodes", nil, "*")
	if good.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: * on a valid list returned %d, want 304", good.Code)
	}
}
