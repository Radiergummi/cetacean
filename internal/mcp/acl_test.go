package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// readOnlyPolicy grants read on the supplied resource patterns to all
// identities. Other reads are denied.
func readOnlyPolicy(resources ...string) *acl.Policy {
	return &acl.Policy{Grants: []acl.Grant{{
		Resources:   resources,
		Audience:    []string{"*"},
		Permissions: []string{"read"},
	}}}
}

func ctxWithIdentity() context.Context {
	return auth.ContextWithIdentity(context.Background(), &auth.Identity{Subject: "tester"})
}

func TestReadServiceResource_ACLDeniesUnpermitted(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "secret-svc"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	if _, err := srv.readResource(ctxWithIdentity(), "cetacean://services/svc1"); err == nil {
		t.Fatal("expected ACL denial for service:secret-svc")
	}
}

func TestReadServiceResource_ACLAllowsPermitted(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "public-api"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	body, err := srv.readResource(ctxWithIdentity(), "cetacean://services/svc1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if !strings.Contains(body, "public-api") {
		t.Errorf("expected service in response, got %s", body)
	}
}

func TestReadServiceList_ACLFilters(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "public-api"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "secret-svc"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	body, err := srv.readResource(ctxWithIdentity(), "cetacean://services")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if !strings.Contains(body, "public-api") {
		t.Errorf("expected public-api in list, got %s", body)
	}
	if strings.Contains(body, "secret-svc") {
		t.Errorf("denied service leaked into list: %s", body)
	}
}

func TestReadSecretResource_ACLDeniesUnpermitted(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "db-password"},
		},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("secret:other-*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	if _, err := srv.readResource(ctxWithIdentity(), "cetacean://secrets/sec1"); err == nil {
		t.Fatal("expected ACL denial for secret:db-password")
	}
}

func TestReadNodeResource_ACLDeniesUnpermitted(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "worker-1"},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("node:manager-*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	if _, err := srv.readResource(ctxWithIdentity(), "cetacean://nodes/node1"); err == nil {
		t.Fatal("expected ACL denial for node:worker-1")
	}
}

func TestReadTaskResource_ACLDeniesUnpermitted(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1", ServiceID: "svc1"})

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("task:other-*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	if _, err := srv.readResource(ctxWithIdentity(), "cetacean://tasks/task1"); err == nil {
		t.Fatal("expected ACL denial for task:task1")
	}
}

func TestReadResource_NoPolicyAllowsAll(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "anything"}},
	})

	// Evaluator with nil policy — Can returns true for everything.
	e := acl.NewEvaluator()

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	if _, err := srv.readResource(ctxWithIdentity(), "cetacean://services/svc1"); err != nil {
		t.Fatalf("expected no denial with nil policy, got %v", err)
	}
}

// The resource returns the newest readable entries, so a burst of unreadable
// ones must not push what the caller may see off the page.
func TestReadHistoryResource_FiltersBeforeThePageIsCut(t *testing.T) {
	c := cache.New(nil)
	c.History().Append(cache.HistoryEntry{Type: cache.EventService, Name: "public-api"})
	for range 150 {
		c.History().Append(cache.HistoryEntry{Type: cache.EventService, Name: "secret-svc"})
	}

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))
	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	body, err := srv.readResource(ctxWithIdentity(), "cetacean://history")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if !strings.Contains(body, "public-api") {
		t.Errorf("the readable entry fell off the page: %s", body)
	}
}

func TestReadHistoryResource_ACLFilters(t *testing.T) {
	c := cache.New(nil)
	c.History().Append(cache.HistoryEntry{
		Type:   cache.EventService,
		Action: "create",
		Name:   "public-api",
	})
	c.History().Append(cache.HistoryEntry{
		Type:   cache.EventService,
		Action: "create",
		Name:   "secret-svc",
	})

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	body, err := srv.readResource(ctxWithIdentity(), "cetacean://history")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if !strings.Contains(body, "public-api") {
		t.Errorf("expected public-api in history, got %s", body)
	}
	if strings.Contains(body, "secret-svc") {
		t.Errorf("denied entry leaked into history: %s", body)
	}
}

// TestReadServiceList_PreservesEmptySliceShape ensures ACL filtering of an
// empty list doesn't cause downstream marshaling surprises.
func TestReadServiceList_PreservesEmptySliceShape(t *testing.T) {
	c := cache.New(nil)
	srv := newResourceTestServer(t, c)

	body, err := srv.readResource(context.Background(), "cetacean://services")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(body), &arr); err != nil {
		t.Fatalf("response should be a JSON array: %v (body %s)", err, body)
	}
}

// TestToolVisibilityForReportsAllowAll pins the three ways every tool stays
// visible: no ACL wired, no identity on the context, and a nil policy.
func TestToolVisibilityForReportsAllowAll(t *testing.T) {
	c := cache.New(nil)

	t.Run("no acl", func(t *testing.T) {
		srv := newResourceTestServer(t, c)

		got := srv.toolVisibilityFor(ctxWithIdentity())
		if got.allow != nil {
			t.Errorf("toolVisibilityFor = %+v, want allow-all", got)
		}
	})

	t.Run("no identity", func(t *testing.T) {
		srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = acl.NewEvaluator() })

		got := srv.toolVisibilityFor(context.Background())
		if got.allow != nil {
			t.Errorf("toolVisibilityFor = %+v, want allow-all", got)
		}
	})

	t.Run("nil policy", func(t *testing.T) {
		srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = acl.NewEvaluator() })

		got := srv.toolVisibilityFor(ctxWithIdentity())
		if got.allow != nil {
			t.Errorf("toolVisibilityFor = %+v, want allow-all", got)
		}
	})
}

// TestToolVisibilityForHidesEverythingGatedWithoutGrants covers an identity
// matching no grant at all. It needs no special case — acl.TypeAccess answers
// false for every type — but the resulting shape is load-bearing for prompts,
// so it is pinned here as well as end to end.
func TestToolVisibilityForHidesEverythingGatedWithoutGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:web-*"},
		Audience:    []string{"user:somebody-else"},
		Permissions: []string{"read"},
	}}})

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

	got := srv.toolVisibilityFor(ctxWithIdentity())
	if got.allow == nil {
		t.Fatal("allow must be set, so gated tools are hidden")
	}

	if got.allow("scale_service") {
		t.Error("a zero-grant identity may not see scale_service")
	}

	if !got.allow("find") {
		t.Error("find is ungated and must stay visible")
	}

	if got.readable("service") {
		t.Error("a zero-grant identity can read no type, which is what hides every prompt")
	}
}

// TestToolVisibilityForAppliesGrants is the ordinary path: a service:read
// grant reveals the gated read and hides the gated write.
func TestToolVisibilityForAppliesGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:*"))

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

	got := srv.toolVisibilityFor(ctxWithIdentity())
	if !got.allow("get_logs") {
		t.Error("service:read must reveal get_logs")
	}

	if got.allow("scale_service") {
		t.Error("service:read must not reveal scale_service")
	}
}

// The reason the projection moved into acl.TypeGrants. A stack grant covers the
// stack's services at call time, so an operator holding one can run get_logs —
// but a projection comparing literal type prefixes sees only "stack" and hides
// the tool, and every prompt driving it, from someone who could use it.
func TestToolVisibilityForExpandsStackGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("stack:web"))

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

	got := srv.toolVisibilityFor(ctxWithIdentity())
	if !got.allow("get_logs") {
		t.Error("a stack:read grant covers the stack's services and must reveal get_logs")
	}

	if !got.readable("service") {
		t.Error("a stack:read grant must make services readable")
	}

	if got.allow("scale_service") {
		t.Error("stack:read is not write; scale_service must stay hidden")
	}

	if got.readable("node") {
		t.Error("nodes belong to no stack; a stack grant must not make them readable")
	}
}

// Pins the second consumer. A notification fan-out doing its own prefix
// comparison gives the same stack-granted caller no service list_changed
// notifications — and gives a caller matching no grant every one of them, since
// PermissionsFor returns nil for "no policy" and "matched nothing" alike.
func TestCanReadAnyOfTypeSharesTheProjection(t *testing.T) {
	identity := &auth.Identity{Subject: "tester"}

	t.Run("stack grant reaches member types", func(t *testing.T) {
		e := acl.NewEvaluator()
		e.SetPolicy(readOnlyPolicy("stack:web"))

		srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

		if !srv.canReadAnyOfType(identity, "service") {
			t.Error("a stack grant covers the stack's services")
		}

		if srv.canReadAnyOfType(identity, "node") {
			t.Error("a stack grant does not cover nodes")
		}
	})

	t.Run("service grant reaches tasks", func(t *testing.T) {
		e := acl.NewEvaluator()
		e.SetPolicy(readOnlyPolicy("service:*"))

		srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

		if !srv.canReadAnyOfType(identity, "task") {
			t.Error("tasks inherit from their parent service")
		}
	})

	t.Run("no grant hears nothing", func(t *testing.T) {
		e := acl.NewEvaluator()
		e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
			Resources:   []string{"service:*"},
			Audience:    []string{"user:somebody-else"},
			Permissions: []string{"read"},
		}}})

		srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

		if srv.canReadAnyOfType(identity, "service") {
			t.Error("a zero-grant caller must not be told a service changed")
		}
	})
}

// find counts every match, not just the returned page, so the count has to be
// taken after the read filter.
func TestFindCountsOnlyReadableBeyondThePage(t *testing.T) {
	c := cache.New(nil)
	for _, name := range []string{"acme-a", "acme-b", "acme-c", "acme-d"} {
		c.SetService(swarm.Service{
			ID:   "id-" + name,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:acme-a"))
	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	td, ok := srv.findTool("find")
	if !ok {
		t.Fatal("find not registered")
	}

	for _, limit := range []int{1, 3} {
		out, err := td.handler(ctxWithIdentity(), newCallToolRequest("find", map[string]any{
			"query": "acme",
			"limit": limit,
		}))
		if err != nil {
			t.Fatalf("handler: %v", err)
		}

		var got findResult
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("unmarshal: %v: %s", err, out)
		}
		if got.Total != 1 || got.Counts["services"] != 1 {
			t.Errorf("limit=%d: total=%d counts=%v, want 1", limit, got.Total, got.Counts)
		}
	}
}

// REST refuses /cluster to an identity holding no grant; the MCP twins of that
// aggregate must refuse it too.
func TestClusterAggregatesRequireAGrant(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"*"},
		Audience:    []string{"user:somebody-else"},
		Permissions: []string{"read"},
	}}})
	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	if _, err := srv.readResource(ctxWithIdentity(), "cetacean://cluster"); err == nil {
		t.Error("cetacean://cluster answered a caller holding no grant")
	}

	td, ok := srv.findTool("get_cluster_status")
	if !ok {
		t.Fatal("get_cluster_status not registered")
	}
	if _, err := td.handler(
		ctxWithIdentity(), newCallToolRequest("get_cluster_status", nil),
	); err == nil {
		t.Error("get_cluster_status answered a caller holding no grant")
	}

	e.SetPolicy(readOnlyPolicy("service:web"))
	if _, err := srv.readResource(ctxWithIdentity(), "cetacean://cluster"); err != nil {
		t.Errorf("cetacean://cluster refused a caller holding a grant: %v", err)
	}
}

// docs/mcp.md promises an unreadable resource reads exactly like a missing one,
// so the error must not differ by class, wording or the name it discloses.
func TestReadDenialLooksLikeNotFound(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "secret-svc"}},
	})

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))
	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	_, denied := srv.readResource(ctxWithIdentity(), "cetacean://services/svc1")
	_, missing := srv.readResource(ctxWithIdentity(), "cetacean://services/svc2")
	if denied == nil || missing == nil {
		t.Fatalf("denied=%v missing=%v, want both to fail", denied, missing)
	}
	if want := strings.ReplaceAll(missing.Error(), "svc2", "svc1"); denied.Error() != want {
		t.Errorf("denied read = %q, want %q", denied, want)
	}

	td, ok := srv.findTool("watch")
	if !ok {
		t.Fatal("watch not registered")
	}
	_, denied = td.handler(ctxWithIdentity(), newCallToolRequest("watch", map[string]any{
		"service": "svc1",
	}))
	_, missing = td.handler(ctxWithIdentity(), newCallToolRequest("watch", map[string]any{
		"service": "svc2",
	}))
	if denied == nil || missing == nil {
		t.Fatalf("denied=%v missing=%v, want both to fail", denied, missing)
	}
	if want := strings.ReplaceAll(missing.Error(), "svc2", "svc1"); denied.Error() != want {
		t.Errorf("denied watch = %q, want %q", denied, want)
	}
}
