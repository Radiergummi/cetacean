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
	"github.com/radiergummi/cetacean/internal/cluster"
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

func TestFilterSearchResults_HidesDeniedServices(t *testing.T) {
	raw := cluster.SearchResults{
		Hits: map[string][]cluster.SearchResult{
			"services": {
				{Type: "services", ID: "svc1", Name: "public-api"},
				{Type: "services", ID: "svc2", Name: "public-web"},
				{Type: "services", ID: "svc3", Name: "secret-svc"},
			},
		},
		Counts: map[string]int{"services": 3},
		Total:  3,
	}

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

	out := srv.filterSearchResults(ctxWithIdentity(), raw)

	hits := out.Hits["services"]
	if len(hits) != 2 {
		t.Fatalf("expected 2 visible service hits, got %d (%+v)", len(hits), hits)
	}
	for _, h := range hits {
		if !strings.HasPrefix(h.Name, "public-") {
			t.Errorf("denied service leaked into search: %s", h.Name)
		}
	}
	if out.Counts["services"] != 2 {
		t.Errorf("services count = %d, want 2", out.Counts["services"])
	}
	if out.Total != 2 {
		t.Errorf("total = %d, want 2", out.Total)
	}
}

func TestFilterSearchResults_NilEvaluatorPassesThrough(t *testing.T) {
	raw := cluster.SearchResults{
		Hits:   map[string][]cluster.SearchResult{"services": {{Name: "anything"}}},
		Counts: map[string]int{"services": 1},
		Total:  1,
	}

	srv := newResourceTestServer(t, cache.New(nil)) // no ACL evaluator

	out := srv.filterSearchResults(ctxWithIdentity(), raw)
	if out.Total != 1 {
		t.Errorf("expected pass-through total 1, got %d", out.Total)
	}
}

func TestFilterSearchResults_TasksKeyOnID(t *testing.T) {
	raw := cluster.SearchResults{
		Hits: map[string][]cluster.SearchResult{
			"tasks": {{Type: "tasks", ID: "anything-task", Name: "web.1"}},
		},
		Counts: map[string]int{"tasks": 1},
		Total:  1,
	}

	e := acl.NewEvaluator()
	// Allow task with a different ID; the fixture's ID does not match.
	e.SetPolicy(readOnlyPolicy("task:other-*"))

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

	out := srv.filterSearchResults(ctxWithIdentity(), raw)
	if hits := out.Hits["tasks"]; len(hits) != 0 {
		t.Errorf("expected task hidden by task: ACL, got %+v", hits)
	}
}

func TestFilterSearchResults_DropsTypeWhenAllDenied(t *testing.T) {
	raw := cluster.SearchResults{
		Hits: map[string][]cluster.SearchResult{
			"services": {{Type: "services", ID: "svc1", Name: "secret-svc"}},
		},
		Counts: map[string]int{"services": 1},
		Total:  1,
	}

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

	out := srv.filterSearchResults(ctxWithIdentity(), raw)
	if _, ok := out.Hits["services"]; ok {
		t.Errorf("type entry should be dropped when all denied, got %+v", out.Hits)
	}
	if out.Total != 0 {
		t.Errorf("total = %d, want 0", out.Total)
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

// TestToolVisibilityForExpandsStackGrants is the reason the projection moved
// into acl.TypeGrants. A stack grant covers the stack's services at call time,
// so an operator holding one can run get_logs — but the old projection
// compared literal type prefixes, saw only "stack", and hid the tool (and
// every prompt driving it) from someone who could use it.
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

// TestCanReadAnyOfTypeSharesTheProjection pins the second consumer. Before,
// notification fan-out did its own prefix comparison, so the same stack-granted
// caller got no service list_changed notifications — and a caller matching no
// grant at all got every one of them, because PermissionsFor returns nil for
// "no policy" and "matched nothing" alike.
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
