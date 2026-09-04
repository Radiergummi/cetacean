# MCP Prompts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Offer six MCP prompts — named investigation and remediation sequences — so an agent knows which order of calls answers a question an operator actually asks.

**Architecture:** A static `promptCatalog()` of `promptDef` values, each declaring the tool names it walks. Operations tier and ACL visibility both *derive* from that declaration rather than being stated separately, so a prompt cannot drift from the tools it instructs. Registration is tier-gated at construction (mirroring `registerTools`); visibility is per-request via `WithPromptFilter`, sharing one extracted predicate with the existing tool filter.

**Tech Stack:** Go 1.26.6, `github.com/mark3labs/mcp-go` v1.0.0 (`mcp.NewPrompt`, `server.AddPrompt`, `server.WithPromptCapabilities`, `server.WithPromptFilter`), stdlib only otherwise.

**Spec:** `docs/specs/2026-09-04-mcp-prompts-design.md`

## Global Constraints

- Package is `internal/mcp`. New files live there.
- **Prompt text carries no backticks.** Go raw string literals are backtick-delimited, and concatenating around them makes the text unreviewable. Tool names and URIs appear bare: `Call get_logs`, not ``Call `get_logs` ``. This is why the constants can be raw strings at all.
- **Prompt text must not assert anything about *this* cluster.** It may describe method; it may not claim the cluster runs three managers or uses overlay networks. Rule 4 of the spec's text conventions, and the one whose breakage is hardest to notice.
- Interpolation is `fmt.Sprintf` with `%s`. No prompt text may contain a literal `%`.
- Prompt handlers read **no** cluster data — no cache read, no Docker call, no resource lookup. They interpolate an argument into static text.
- A prompt's tier is `max(tier of each driven tool)`; its visibility is "every driven tool is visible to this caller". Never hardcode either.
- Run `gofmt -w` on every file you touch. `golangci-lint run ./internal/...` must report `0 issues` before each commit.
- `new(expr)` is valid and lint-*required* on Go 1.26 (`modernize` rejects a `ptrTo` helper). Do not "fix" it to a helper.
- Comments explain *why*, not *what*. Match the density of the surrounding file.

**Batches.** Task 1–4 are Batch A (mechanism plus the three tier-0 diagnostic prompts) and leave a coherent, shippable feature. Task 5–6 are Batch B (the three remediation prompts, then docs). Reviewable independently, in order.

---

### Task 1: Extract the tool-visibility predicate

Pure refactor, no behaviour change. `filterToolsForIdentity` builds `allowedByType` inline and passes a closure to `filterToolsByACLSpec`. The prompt filter (Task 4) needs the same closure plus one fact the current code discards: whether the identity holds *zero* grants. Extracting it now keeps Task 4 from copying the grant-flattening logic, including the `write` implies `read` rule and the `*` wildcard.

**Files:**
- Modify: `internal/mcp/server.go` (`filterToolsForIdentity`, currently around line 413)
- Test: `internal/mcp/acl_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `type toolVisibility struct { allow func(name string) bool; noGrants bool }` and `func (s *Server) toolVisibilityFor(ctx context.Context) toolVisibility`. A zero `toolVisibility` (`allow == nil`, `noGrants == false`) means "everything is visible".

- [ ] **Step 1: Write the failing test**

Append to `internal/mcp/acl_test.go`:

```go
// TestToolVisibilityForReportsAllowAll pins the three ways every tool stays
// visible: no ACL wired, no identity on the context, and a nil policy.
func TestToolVisibilityForReportsAllowAll(t *testing.T) {
	c := cache.New(nil)

	t.Run("no acl", func(t *testing.T) {
		srv := newResourceTestServer(t, c)

		got := srv.toolVisibilityFor(ctxWithIdentity())
		if got.allow != nil || got.noGrants {
			t.Errorf("toolVisibilityFor = %+v, want allow-all", got)
		}
	})

	t.Run("no identity", func(t *testing.T) {
		srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = acl.NewEvaluator() })

		got := srv.toolVisibilityFor(context.Background())
		if got.allow != nil || got.noGrants {
			t.Errorf("toolVisibilityFor = %+v, want allow-all", got)
		}
	})

	t.Run("nil policy", func(t *testing.T) {
		srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = acl.NewEvaluator() })

		got := srv.toolVisibilityFor(ctxWithIdentity())
		if got.allow != nil || got.noGrants {
			t.Errorf("toolVisibilityFor = %+v, want allow-all", got)
		}
	})
}

// TestToolVisibilityForReportsNoGrants covers the fact filterToolsForIdentity
// discards today: an identity matching no grant at all. Prompts need it, since
// a caller who can read nothing has no investigation to seed.
func TestToolVisibilityForReportsNoGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:web-*"},
		Audience:    []string{"user:somebody-else"},
		Permissions: []string{"read"},
	}}})

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

	got := srv.toolVisibilityFor(ctxWithIdentity())
	if !got.noGrants {
		t.Error("toolVisibilityFor did not report zero grants for an unmatched identity")
	}

	if got.allow == nil {
		t.Fatal("allow must still be set, so gated tools are hidden")
	}

	if got.allow("scale_service") {
		t.Error("a zero-grant identity may not see scale_service")
	}

	if !got.allow("search") {
		t.Error("search is ungated and must stay visible")
	}
}

// TestToolVisibilityForAppliesGrants is the ordinary path: a service:read
// grant reveals the gated read and hides the gated write.
func TestToolVisibilityForAppliesGrants(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:*"))

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = e })

	got := srv.toolVisibilityFor(ctxWithIdentity())
	if got.noGrants {
		t.Fatal("identity holds a grant; noGrants must be false")
	}

	if !got.allow("get_logs") {
		t.Error("service:read must reveal get_logs")
	}

	if got.allow("scale_service") {
		t.Error("service:read must not reveal scale_service")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestToolVisibilityFor 2>&1 | head -20`
Expected: FAIL to build — `srv.toolVisibilityFor undefined`, `got.allow undefined`.

- [ ] **Step 3: Extract the predicate**

In `internal/mcp/server.go`, replace the whole body of `filterToolsForIdentity` and add the new type and method above it:

```go
// toolVisibility describes which tools an identity may see. allow is nil when
// every tool is visible — no ACL wired, no identity, or a nil policy, all of
// which mirror acl.Evaluator.Can's allow-all behaviour. noGrants marks an
// identity that matched no grant at all; it still gets an allow func (the
// ungated tools stay visible, as they do in tools/list) but prompts treat it
// as a floor, since a caller who can read nothing has nothing to investigate.
type toolVisibility struct {
	allow    func(name string) bool
	noGrants bool
}

// toolVisibilityFor flattens an identity's grants into a per-tool predicate.
// Shared by filterToolsForIdentity and filterPromptsForIdentity so the
// grant-flattening rules — "write" implies "read", "*" wildcards, and the
// always-visible ungated tools — are stated exactly once.
func (s *Server) toolVisibilityFor(ctx context.Context) toolVisibility {
	if s.acl == nil {
		return toolVisibility{}
	}

	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return toolVisibility{}
	}

	perms := s.acl.PermissionsFor(identity)
	if perms == nil {
		// nil policy = allow all (mirrors acl.Evaluator.Can).
		return toolVisibility{}
	}

	if len(perms) == 0 {
		// Identity has zero grants. Drop everything gated; ungated tools
		// (search and the cross-type reads) stay visible, since each
		// ACL-filters its own results at call time.
		return toolVisibility{
			noGrants: true,
			allow: func(name string) bool {
				_, gated := toolACLSpecs[name]
				return !gated
			},
		}
	}

	allowedByType := make(map[string]map[string]bool, 2) // permission → type → bool
	for pattern, perms := range perms {
		resType, _, ok := splitResourceType(pattern)
		if !ok {
			continue
		}
		for _, perm := range perms {
			byType, ok := allowedByType[perm]
			if !ok {
				byType = make(map[string]bool)
				allowedByType[perm] = byType
			}
			byType[resType] = true
		}
	}

	// "write" implies "read" — mirror Evaluator.hasPermission.
	if writers, ok := allowedByType["write"]; ok {
		readers := allowedByType["read"]
		if readers == nil {
			readers = make(map[string]bool, len(writers))
			allowedByType["read"] = readers
		}
		for t := range writers {
			readers[t] = true
		}
	}

	return toolVisibility{allow: func(name string) bool {
		spec, gated := toolACLSpecs[name]
		if !gated {
			return true
		}
		if spec.resourceType == "*" {
			// Tool acts across all types — visible if any grant exists.
			return true
		}
		byType := allowedByType[spec.permission]
		if byType == nil {
			return false
		}
		return byType[spec.resourceType] || byType["*"]
	}}
}
```

Then reduce `filterToolsForIdentity` to:

```go
// filterToolsForIdentity is wired as a WithToolFilter for tools/list. Tier
// gating already happened at registration; this filter additionally hides
// tools whose primary resource type the identity has zero grants on, so the
// catalog reflects what the caller can actually invoke. The filter is
// advisory — call-time ACL still enforces, so returning the full slice would
// never grant anything; the goal here is a truthful list.
func (s *Server) filterToolsForIdentity(ctx context.Context, tools []mcplib.Tool) []mcplib.Tool {
	visibility := s.toolVisibilityFor(ctx)
	if visibility.allow == nil {
		return tools
	}

	return filterToolsByACLSpec(tools, visibility.allow)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -count=1 2>&1 | tail -5`
Expected: PASS. The pre-existing tool-filter tests must pass unchanged — this is a refactor, and any change in their result means the extraction altered behaviour.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/mcp/server.go internal/mcp/acl_test.go
golangci-lint run ./internal/...
git add internal/mcp/server.go internal/mcp/acl_test.go
git commit -m "refactor(mcp): extract the tool-visibility predicate from the tools filter

Prompts need the same grant flattening the tools filter does, plus one fact it
discards: whether the identity matched no grant at all. Extracting it keeps the
prompt filter from carrying a second copy of the write-implies-read and wildcard
rules. No behaviour change; the existing filter tests pass untouched."
```

---

### Task 2: The prompt catalog and the three diagnostic prompts

Catalog, text constants, and tier derivation. Nothing is registered yet, so this task's tests exercise the catalog directly — including the drift guard that makes the whole derive-from-tools approach safe.

**Files:**
- Create: `internal/mcp/prompts.go`
- Create: `internal/mcp/prompt_text.go`
- Test: Create `internal/mcp/prompts_test.go`

**Interfaces:**
- Consumes: `toolDef` and `s.toolCatalog()` from `internal/mcp/tools.go`; `config.OperationsLevel`, `config.OpsReadOnly`.
- Produces:
  - `type promptDef struct { prompt mcplib.Prompt; drives []string; handler mcpserver.PromptHandlerFunc }`
  - `func promptCatalog() []promptDef`
  - `func (s *Server) toolTiers() map[string]config.OperationsLevel`
  - `func promptTier(def promptDef, tiers map[string]config.OperationsLevel) config.OperationsLevel`
  - `func requiredArgument(req mcplib.GetPromptRequest, name string) (string, error)`
  - Text constants `promptTextDiagnoseService`, `promptTextExplainUnschedulable`, `promptTextReviewCapacity`.

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/prompts_test.go`:

```go
package mcp

import (
	"context"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// TestPromptCatalogDrivesOnlyRealTools is the guard that makes deriving tier
// and ACL visibility from `drives` safe. A renamed or removed tool would
// otherwise silently mis-tier a prompt (an unknown name contributes no tier)
// or hide it from every caller forever.
func TestPromptCatalogDrivesOnlyRealTools(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))
	tiers := srv.toolTiers()

	for _, def := range promptCatalog() {
		for _, tool := range def.drives {
			if _, ok := tiers[tool]; !ok {
				t.Errorf("prompt %q drives %q, which is not in toolCatalog()",
					def.prompt.Name, tool)
			}
		}
	}
}

// TestPromptTierIsTheMaxOfItsTools pins the derivation rule. Hardcoding a tier
// would let a prompt advertise itself below the tier of a mutation it
// instructs.
func TestPromptTierIsTheMaxOfItsTools(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))
	tiers := srv.toolTiers()

	want := map[string]config.OperationsLevel{
		"diagnose_service":      config.OpsReadOnly,
		"explain_unschedulable": config.OpsReadOnly,
		"review_capacity":       config.OpsReadOnly,
	}

	for _, def := range promptCatalog() {
		expected, tracked := want[def.prompt.Name]
		if !tracked {
			continue
		}

		if got := promptTier(def, tiers); got != expected {
			t.Errorf("prompt %q tier = %v, want %v", def.prompt.Name, got, expected)
		}
	}
}

// TestPromptCatalogDeclaresItsArguments — the argument names are what #170's
// completion provider will key off, so they are part of the contract.
func TestPromptCatalogDeclaresItsArguments(t *testing.T) {
	want := map[string]string{
		"diagnose_service":      "service",
		"explain_unschedulable": "service",
		"review_capacity":       "",
	}

	for _, def := range promptCatalog() {
		expected, tracked := want[def.prompt.Name]
		if !tracked {
			continue
		}

		if expected == "" {
			if len(def.prompt.Arguments) != 0 {
				t.Errorf("prompt %q takes %d arguments, want none",
					def.prompt.Name, len(def.prompt.Arguments))
			}

			continue
		}

		if len(def.prompt.Arguments) != 1 {
			t.Fatalf("prompt %q has %d arguments, want 1",
				def.prompt.Name, len(def.prompt.Arguments))
		}

		arg := def.prompt.Arguments[0]
		if arg.Name != expected {
			t.Errorf("prompt %q argument = %q, want %q", def.prompt.Name, arg.Name, expected)
		}

		if !arg.Required {
			t.Errorf("prompt %q argument %q must be required", def.prompt.Name, arg.Name)
		}
	}
}

// TestDiagnoseServiceExpandsItsArgument is the wording guard: the expanded
// messages are asserted so the text is reviewable and cannot drift silently.
func TestDiagnoseServiceExpandsItsArgument(t *testing.T) {
	def := findPrompt(t, "diagnose_service")

	result, err := def.handler(context.Background(), getPromptRequest(
		"diagnose_service",
		map[string]string{"service": "web"},
	))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(result.Messages))
	}

	message := result.Messages[0]
	if message.Role != mcplib.RoleUser {
		t.Errorf("role = %q, want %q", message.Role, mcplib.RoleUser)
	}

	text, ok := message.Content.(mcplib.TextContent)
	if !ok {
		t.Fatalf("content is %T, want mcplib.TextContent", message.Content)
	}

	if !strings.Contains(text.Text, "web") {
		t.Error("expanded text does not name the service")
	}

	// The sequence is the content: every driven tool must appear by name, or
	// the prompt has told the model nothing the tool list did not.
	for _, tool := range def.drives {
		if !strings.Contains(text.Text, tool) {
			t.Errorf("expanded text does not name the %q tool it drives", tool)
		}
	}
}

// TestPromptHandlerRejectsAMissingArgument — mcp-go does not enforce
// PromptArgument.Required, so a client may omit it. Interpolating an empty
// string would seed a conversation about the service named "".
func TestPromptHandlerRejectsAMissingArgument(t *testing.T) {
	def := findPrompt(t, "diagnose_service")

	for name, args := range map[string]map[string]string{
		"absent": {},
		"empty":  {"service": ""},
		"blank":  {"service": "   "},
	} {
		if _, err := def.handler(
			context.Background(),
			getPromptRequest("diagnose_service", args),
		); err == nil {
			t.Errorf("%s argument: handler returned no error", name)
		}
	}
}

// TestPromptTextMakesNoClusterClaims enforces rule 4 of the text conventions.
// A prompt asserting something false about this cluster teaches the model a
// wrong premise it will then act on, and the text is static so it cannot know.
func TestPromptTextMakesNoClusterClaims(t *testing.T) {
	// Phrases that can only be true of a particular cluster.
	banned := []string{
		"your cluster has",
		"this cluster has",
		"the cluster runs",
		"there are three",
	}

	for _, def := range promptCatalog() {
		result, err := def.handler(context.Background(), getPromptRequest(
			def.prompt.Name,
			map[string]string{"service": "example", "node": "example"},
		))
		if err != nil {
			t.Fatalf("prompt %q handler: %v", def.prompt.Name, err)
		}

		text, ok := result.Messages[0].Content.(mcplib.TextContent)
		if !ok {
			t.Fatalf("prompt %q content is %T", def.prompt.Name, result.Messages[0].Content)
		}

		lowered := strings.ToLower(text.Text)
		for _, phrase := range banned {
			if strings.Contains(lowered, phrase) {
				t.Errorf("prompt %q asserts %q about this cluster", def.prompt.Name, phrase)
			}
		}
	}
}

// TestPromptTextCarriesNoBackticks — the constants are Go raw strings, which
// are backtick-delimited, so a backtick cannot appear in one. A contributor
// adding markdown code spans would have to break the literal to do it; this
// fails first and says why.
func TestPromptTextCarriesNoBackticks(t *testing.T) {
	for _, def := range promptCatalog() {
		result, err := def.handler(context.Background(), getPromptRequest(
			def.prompt.Name,
			map[string]string{"service": "example", "node": "example"},
		))
		if err != nil {
			t.Fatalf("prompt %q handler: %v", def.prompt.Name, err)
		}

		text, ok := result.Messages[0].Content.(mcplib.TextContent)
		if !ok {
			t.Fatalf("prompt %q content is %T", def.prompt.Name, result.Messages[0].Content)
		}

		if strings.Contains(text.Text, "`") {
			t.Errorf("prompt %q text contains a backtick; the constants are raw strings",
				def.prompt.Name)
		}
	}
}

// findPrompt returns the catalog entry under name.
func findPrompt(t *testing.T, name string) promptDef {
	t.Helper()

	for _, def := range promptCatalog() {
		if def.prompt.Name == name {
			return def
		}
	}

	t.Fatalf("prompt %q is not in promptCatalog()", name)

	return promptDef{}
}

// getPromptRequest builds a prompts/get request for a handler called directly.
func getPromptRequest(name string, args map[string]string) mcplib.GetPromptRequest {
	return mcplib.GetPromptRequest{
		Params: mcplib.GetPromptParams{
			Name:      name,
			Arguments: args,
		},
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestPrompt 2>&1 | head -20`
Expected: FAIL to build — `undefined: promptCatalog`, `undefined: promptTier`, `srv.toolTiers undefined`.

- [ ] **Step 3: Write the prompt text**

Create `internal/mcp/prompt_text.go`:

```go
package mcp

// Prompt message text. This is the closest thing Cetacean has to telling a
// model how to reason about a Swarm cluster, so it lives here — reviewable in
// one place, beside mcpInstructions in review terms — rather than inline in the
// catalog.
//
// Four rules, all enforced by tests in prompts_test.go:
//
//  1. Name the driven tools explicitly, in order. The sequence is the content;
//     a prompt that gestures at "investigate the service" adds nothing to the
//     tool list the model already has.
//  2. State the stopping condition — what to report instead of continuing.
//  3. A remediation prompt must not presume its own diagnosis. Each opens by
//     confirming the problem is real, and says to stop and report if it is not.
//  4. Make no claims about *this* cluster. The text is static and cannot know
//     the cluster's shape. Describe method; never assert that the cluster runs
//     three managers or uses overlay networks.
//
// No backticks: these are raw string literals, which are backtick-delimited.
// Tool names appear bare. No literal % either — the single %s is the
// interpolated argument.
const (
	promptTextDiagnoseService = `A service in this Swarm cluster is reported unhealthy: %s.

Work through this order and stop as soon as you can name the cause.

1. Resolve the name with the search tool to get the service ID, then read
   cetacean://services/<id> for its mode, desired replicas and update status.
2. Call list_resources for tasks and find this service's tasks. Compare the
   running count against desired. Note any task in failed, rejected or
   orphaned, and its Status.Err.
3. For the most recent failing task, call get_logs on the service. A crash loop
   usually names itself in the last lines before exit.
4. If the tasks are running but the service still misbehaves, call get_metrics
   for cpu and memory over the last hour. A container at its memory limit is
   killed and restarted without ever logging why.
5. Read cetacean://history for changes to this service. A fault that started
   minutes after an image update is usually the update.
6. Call get_recommendations and report any finding naming this service.

Report the cause, the evidence you used, and what you would change. Do not
apply a change as part of this investigation.`

	promptTextExplainUnschedulable = `Service %s has tasks Swarm has not placed. Four independent causes produce
this one symptom; check them in this order, because the cheap checks rule out
the expensive ones.

1. Resolve the name with the search tool, then read cetacean://services/<id>
   and note Spec.TaskTemplate.Placement - constraints, preferences and
   MaxReplicas.
2. Call list_resources for tasks, filter to this service, and read the pending
   task's Status.Err and Status.Message. Swarm often states the reason outright
   ("no suitable node"); if it does, stop there.
3. Call list_resources for nodes. For each placement constraint, check which
   nodes satisfy it - node.labels.* come from a node's Spec.Labels, node.role
   and node.hostname from its description. A constraint naming a label no node
   carries can never be satisfied.
4. Check each node's Availability. A drain node accepts nothing and a pause
   node accepts no new tasks, so a cluster can look healthy and still have
   nowhere to put this service.
5. Compare the task template's resource reservations against what nodes have
   left. Reservations, not limits, decide placement.

Report which of the four it is, and the specific constraint, label or
reservation responsible. If several apply, say so - fixing one leaves the task
pending.`

	promptTextReviewCapacity = `Review whether this Swarm cluster has capacity headroom.

1. Read cetacean://cluster for the node count and aggregate capacity.
2. Call list_resources for nodes and note each node's role, availability and
   resources. Manager nodes running workloads are a resilience risk even when
   they have room.
3. Call get_metrics for each node's cpu and memory over the last day, or the
   last week if load is weekly. Compare actual use against reservations rather
   than limits: Swarm schedules on reservations, so a cluster can refuse to
   place a task while sitting mostly idle.
4. Call get_recommendations and report every sizing and cluster finding.
5. State the headroom as "the largest service that could still be placed", not
   as a percentage - a percentage hides fragmentation across nodes.

Report where the cluster is constrained, which nodes are the constraint, and
whether the limit is reservations or real use. Do not change any reservation as
part of this review.`
)
```

- [ ] **Step 4: Write the catalog**

Create `internal/mcp/prompts.go`:

```go
package mcp

import (
	"context"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/radiergummi/cetacean/internal/config"
)

// promptDef is a prompt plus the tools it walks. drives is load-bearing: both
// the prompt's operations tier and its per-caller visibility derive from it, so
// a prompt cannot advertise itself below the tier of a mutation it instructs,
// and cannot be offered to a caller who could not complete it.
//
// The order of drives is the order the text walks them, which keeps the
// declaration readable against the prompt body.
type promptDef struct {
	prompt  mcplib.Prompt
	drives  []string
	handler mcpserver.PromptHandlerFunc
}

// promptCatalog is the static set of prompts. A free function rather than a
// Server method because handlers read no cluster data — they interpolate an
// argument into static text, so there is nothing to close over.
func promptCatalog() []promptDef {
	return []promptDef{
		{
			prompt: mcplib.NewPrompt("diagnose_service",
				mcplib.WithPromptTitle("Diagnose an unhealthy service"),
				mcplib.WithPromptDescription(
					"Walk a service's tasks, the failing task's logs, its metrics and "+
						"recent changes to find why it is unhealthy. Reads only.",
				),
				mcplib.WithArgument("service",
					mcplib.ArgumentTitle("Service"),
					mcplib.ArgumentDescription("Name or ID of the unhealthy service"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{
				"search",
				"list_resources",
				"get_logs",
				"get_metrics",
				"get_recommendations",
			},
			handler: interpolatingHandler(
				"Diagnose an unhealthy service",
				promptTextDiagnoseService,
				"service",
			),
		},
		{
			prompt: mcplib.NewPrompt("explain_unschedulable",
				mcplib.WithPromptTitle("Explain why a service will not schedule"),
				mcplib.WithPromptDescription(
					"Separate the four causes of an unplaced task: placement constraints, "+
						"node labels, node availability, and resource reservations. Reads only.",
				),
				mcplib.WithArgument("service",
					mcplib.ArgumentTitle("Service"),
					mcplib.ArgumentDescription("Name or ID of the service that will not schedule"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{"search", "list_resources"},
			handler: interpolatingHandler(
				"Explain why a service will not schedule",
				promptTextExplainUnschedulable,
				"service",
			),
		},
		{
			prompt: mcplib.NewPrompt("review_capacity",
				mcplib.WithPromptTitle("Review cluster capacity"),
				mcplib.WithPromptDescription(
					"Join node capacity, reservations, real usage and sizing findings to say "+
						"where the cluster is constrained. Reads only.",
				),
			),
			drives: []string{"list_resources", "get_metrics", "get_recommendations"},
			handler: staticHandler(
				"Review cluster capacity",
				promptTextReviewCapacity,
			),
		},
	}
}

// interpolatingHandler builds a handler that substitutes one required argument
// into the text. The argument is not checked against the cache: the text tells
// the model to resolve the name with search first, which is what
// mcpInstructions already asks of every client, and failing prompts/get on a
// typo leaves the client no way to correct it.
func interpolatingHandler(description, text, argument string) mcpserver.PromptHandlerFunc {
	return func(
		_ context.Context,
		req mcplib.GetPromptRequest,
	) (*mcplib.GetPromptResult, error) {
		value, err := requiredArgument(req, argument)
		if err != nil {
			return nil, err
		}

		return promptResult(description, fmt.Sprintf(text, value)), nil
	}
}

// staticHandler builds a handler for a prompt that takes no arguments.
func staticHandler(description, text string) mcpserver.PromptHandlerFunc {
	return func(
		_ context.Context,
		_ mcplib.GetPromptRequest,
	) (*mcplib.GetPromptResult, error) {
		return promptResult(description, text), nil
	}
}

// promptResult wraps text as the single user message a prompt expands to. One
// message, and never a fabricated assistant turn: the host is seeding a
// conversation, not replaying one.
func promptResult(description, text string) *mcplib.GetPromptResult {
	return mcplib.NewGetPromptResult(description, []mcplib.PromptMessage{
		mcplib.NewPromptMessage(mcplib.RoleUser, mcplib.NewTextContent(text)),
	})
}

// requiredArgument reads an argument a prompt cannot expand without. mcp-go
// does not enforce PromptArgument.Required, so a client may omit it; without
// this the text would interpolate an empty string and seed a conversation
// about the service named "".
func requiredArgument(req mcplib.GetPromptRequest, name string) (string, error) {
	value := strings.TrimSpace(req.Params.Arguments[name])
	if value == "" {
		return "", fmt.Errorf("prompt argument %q is required", name)
	}

	return value, nil
}

// toolTiers maps every tool name to its operations tier, for deriving a
// prompt's tier from the tools it drives.
func (s *Server) toolTiers() map[string]config.OperationsLevel {
	catalog := s.toolCatalog()

	tiers := make(map[string]config.OperationsLevel, len(catalog))
	for _, td := range catalog {
		tiers[td.tool.Name] = td.tier
	}

	return tiers
}

// promptTier is the highest tier among the tools a prompt drives, so a prompt
// is never offered at a tier that would refuse one of its steps.
//
// A name absent from tiers contributes nothing rather than failing loudly —
// there is nowhere sensible to fail from a pure function called during
// registration. TestPromptCatalogDrivesOnlyRealTools is what catches it, and
// catches it at the only time it can be fixed.
func promptTier(def promptDef, tiers map[string]config.OperationsLevel) config.OperationsLevel {
	tier := config.OpsReadOnly
	for _, name := range def.drives {
		if got, ok := tiers[name]; ok && got > tier {
			tier = got
		}
	}

	return tier
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -run TestPrompt -count=1 -v 2>&1 | tail -25`
Expected: PASS for all seven prompt tests.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/mcp/prompts.go internal/mcp/prompt_text.go internal/mcp/prompts_test.go
golangci-lint run ./internal/...
git add internal/mcp/prompts.go internal/mcp/prompt_text.go internal/mcp/prompts_test.go
git commit -m "feat(mcp): add the prompt catalog and three diagnostic prompts

A prompt declares the tools it walks, and its operations tier derives from
that, so it can never advertise itself below the tier of a step it instructs.
Nothing is registered yet.

The text lives in its own file under four rules, three of which are enforced by
tests: every driven tool is named, no claim is made about this particular
cluster, and no backticks (the constants are raw strings)."
```

---

### Task 3: Register the prompts and wire the capability

`AddPrompt` does not enable the capability — `prompts/list` and `prompts/get` are gated on `capabilities.prompts != nil`, which only `WithPromptCapabilities` sets. That is the advertise-versus-wire split that produced E-1 and E-3 during the 2026-07-28 upgrade, so the test drives the real transport rather than the catalog.

**Files:**
- Modify: `internal/mcp/server.go` (`Server` struct fields near line 99; `serverOptions` near line 181; `New` near line 250)
- Modify: `internal/mcp/prompts.go` (add `registerPrompts`)
- Test: Create `internal/mcp/prompts_e2e_test.go`

**Interfaces:**
- Consumes: `promptCatalog`, `promptTier`, `s.toolTiers` from Task 2.
- Produces: `s.registeredPrompts []promptDef` on `Server`; `func (s *Server) registerPrompts()`.

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/prompts_e2e_test.go`:

```go
package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// promptListing is the prompts/list result.
type promptListing struct {
	Prompts []struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Arguments   []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"arguments"`
	} `json:"prompts"`
}

// listPrompts drives prompts/list through the real transport.
func listPrompts(t *testing.T, handler http.Handler) promptListing {
	t.Helper()

	_, envelope := mcpModern(t, handler, 1, "prompts/list", `{}`)
	if envelope.Error != nil {
		t.Fatalf("prompts/list failed: %+v", envelope.Error)
	}

	var listing promptListing
	if err := json.Unmarshal(envelope.Result, &listing); err != nil {
		t.Fatalf("decode prompts/list: %v (raw %s)", err, envelope.Result)
	}

	return listing
}

// promptNames returns the listed prompt names, for asserting by name rather
// than by count — a prompt moving tier should fail visibly.
func promptNames(listing promptListing) []string {
	names := make([]string, 0, len(listing.Prompts))
	for _, prompt := range listing.Prompts {
		names = append(names, prompt.Name)
	}

	return names
}

// TestPromptsCapabilityIsWired is the E-1/E-3 guard. A unit test on
// promptCatalog() passes happily while WithPromptCapabilities is unwired, at
// which point mcp-go answers every prompts/* call with METHOD_NOT_FOUND — the
// server would list prompts nowhere and serve none.
func TestPromptsCapabilityIsWired(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	listing := listPrompts(t, handler)
	if len(listing.Prompts) == 0 {
		t.Fatal("prompts/list returned nothing; the capability is not wired")
	}

	_, envelope := mcpModern(t, handler, 2, "prompts/get",
		`{"name":"review_capacity","arguments":{}}`)
	if envelope.Error != nil {
		t.Fatalf("prompts/get failed: %+v", envelope.Error)
	}
}

// TestPromptsListIsTierGated — at tier 0 only the diagnostic prompts exist.
// Asserted by name so a prompt changing tier fails here rather than silently
// appearing in a read-only deployment.
func TestPromptsListIsTierGated(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	got := promptNames(listPrompts(t, handler))
	want := map[string]bool{
		"diagnose_service":      true,
		"explain_unschedulable": true,
		"review_capacity":       true,
	}

	if len(got) != len(want) {
		t.Fatalf("tier 0 listed %v, want exactly %d diagnostic prompts", got, len(want))
	}

	for _, name := range got {
		if !want[name] {
			t.Errorf("tier 0 listed %q, which is not a tier-0 prompt", name)
		}
	}
}

// TestPromptsGetExpandsThroughTheTransport confirms the handler is reachable
// and its argument survives JSON round-tripping.
func TestPromptsGetExpandsThroughTheTransport(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	_, envelope := mcpModern(t, handler, 1, "prompts/get",
		`{"name":"diagnose_service","arguments":{"service":"web"}}`)
	if envelope.Error != nil {
		t.Fatalf("prompts/get failed: %+v", envelope.Error)
	}

	var result struct {
		Messages []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode prompts/get: %v (raw %s)", err, envelope.Result)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(result.Messages))
	}

	if result.Messages[0].Role != "user" {
		t.Errorf("role = %q, want user", result.Messages[0].Role)
	}

	if want := "web"; !strings.Contains(result.Messages[0].Content.Text, want) {
		t.Errorf("expanded text does not name %q", want)
	}
}

// TestPromptsGetRejectsAMissingArgument — the handler's guard has to survive
// the transport, since mcp-go does not enforce Required itself.
func TestPromptsGetRejectsAMissingArgument(t *testing.T) {
	handler := newPromptTestServer(t, config.OpsReadOnly).Handler()

	_, envelope := mcpModern(t, handler, 1, "prompts/get",
		`{"name":"diagnose_service","arguments":{}}`)
	if envelope.Error == nil {
		t.Fatal("prompts/get with no argument succeeded; the service name is required")
	}
}

// newPromptTestServer wires a server at the given operations level.
func newPromptTestServer(t *testing.T, opsLevel config.OperationsLevel) *Server {
	t.Helper()

	return newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, opsLevel)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestPrompts 2>&1 | head -20`
Expected: FAIL — `prompts/list failed` with a `METHOD_NOT_FOUND` error, because neither the capability nor the prompts are wired.

- [ ] **Step 3: Add the struct field**

In `internal/mcp/server.go`, beside `registeredTools` (around line 99):

```go
	// registeredPrompts holds the prompts that passed the tier gate, so the
	// per-identity prompts/list filter (filterPromptsForIdentity) can read
	// each one's driven tools back.
	registeredPrompts []promptDef
```

- [ ] **Step 4: Write registerPrompts**

Append to `internal/mcp/prompts.go`:

```go
// registerPrompts registers every prompt the deployment's operations level
// permits. Tier gating happens here, once, exactly as registerTools does it;
// per-caller ACL visibility is a request-time filter.
func (s *Server) registerPrompts() {
	tiers := s.toolTiers()
	opsLevel := s.config.EffectiveOperationsLevel(s.globalOpsLevel)

	catalog := promptCatalog()

	s.registeredPrompts = make([]promptDef, 0, len(catalog))
	for _, def := range catalog {
		if opsLevel < promptTier(def, tiers) {
			continue
		}

		s.registeredPrompts = append(s.registeredPrompts, def)
		s.mcpServer.AddPrompt(def.prompt, def.handler)
	}
}
```

- [ ] **Step 5: Wire the capability and the call**

In `internal/mcp/server.go`, add to `serverOptions` after `WithToolFilter` (around line 183):

```go
		// AddPrompt does NOT enable the capability: mcp-go gates prompts/list
		// and prompts/get on capabilities.prompts != nil, which only this
		// option sets. Registering prompts without it serves none of them —
		// the same advertise-versus-wire split that made server/discover
		// promise Tasks and UI nothing backed. TestPromptsCapabilityIsWired
		// drives the real transport for exactly this reason.
		//
		// listChanged is false: the catalog is static and cannot change while
		// the process runs.
		mcpserver.WithPromptCapabilities(false),
```

Then in `New`, immediately after `srv.registerTools()` (around line 250):

```go
	srv.registerPrompts()
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -count=1 2>&1 | tail -5`
Expected: PASS, whole package.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/mcp/prompts.go internal/mcp/server.go internal/mcp/prompts_e2e_test.go
golangci-lint run ./internal/...
git add internal/mcp/prompts.go internal/mcp/server.go internal/mcp/prompts_e2e_test.go
git commit -m "feat(mcp): register prompts and advertise the capability

AddPrompt does not enable the capability — mcp-go gates prompts/list and
prompts/get on capabilities.prompts, which only WithPromptCapabilities sets, so
registering without it serves nothing. That is the split that made
server/discover promise Tasks and UI no capability backed, so the tests drive
the real transport rather than the catalog."
```

---

### Task 4: Filter prompts by the caller's ACL

A prompt is a sequence, so one unavailable tool hides it: offering a runbook the caller cannot finish is worse than offering none, and for a remediation prompt it can dead-end after the model has already written. Plus the zero-grant floor, the one case where "every tool is visible" fails to imply "the prompt is useful".

**Files:**
- Modify: `internal/mcp/server.go` (`serverOptions`; add `filterPromptsForIdentity` beside `filterToolsForIdentity`)
- Test: `internal/mcp/prompts_e2e_test.go`

**Interfaces:**
- Consumes: `toolVisibility` and `s.toolVisibilityFor` from Task 1; `s.registeredPrompts` from Task 3.
- Produces: `func (s *Server) filterPromptsForIdentity(ctx context.Context, prompts []mcplib.Prompt) []mcplib.Prompt`.

- [ ] **Step 1: Write the failing test**

Append to `internal/mcp/prompts_e2e_test.go` (add imports `github.com/radiergummi/cetacean/internal/acl` and `github.com/radiergummi/cetacean/internal/auth`):

```go
// listPromptsAs drives prompts/list with an identity on the request context,
// which is how the ACL filter sees a caller.
func listPromptsAs(t *testing.T, srv *Server, identity *auth.Identity) promptListing {
	t.Helper()

	request := modernRequest(t, 1, "prompts/list", `{}`)
	request = request.WithContext(auth.ContextWithIdentity(request.Context(), identity))

	_, envelope := sendMCP(t, srv.Handler(), request)
	if envelope.Error != nil {
		t.Fatalf("prompts/list failed: %+v", envelope.Error)
	}

	var listing promptListing
	if err := json.Unmarshal(envelope.Result, &listing); err != nil {
		t.Fatalf("decode prompts/list: %v (raw %s)", err, envelope.Result)
	}

	return listing
}

// TestPromptsHiddenWhenADrivenToolIsDenied — diagnose_service walks get_logs,
// which needs service:read. A caller with only node grants cannot complete the
// sequence, so it must not be offered.
func TestPromptsHiddenWhenADrivenToolIsDenied(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("node:*"))

	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsImpactful,
		func(o *Options) { o.ACL = evaluator },
	)

	got := promptNames(listPromptsAs(t, srv, &auth.Identity{Subject: "tester"}))
	for _, name := range got {
		if name == "diagnose_service" {
			t.Error("diagnose_service offered to a caller with no service:read grant")
		}

		if name == "drain_node" {
			t.Error("drain_node offered to a caller with only node:read, not node:write")
		}
	}
}

// TestPromptsVisibleWhenEveryDrivenToolIs — the same caller with service:read
// gets the diagnostic prompts and still not the remediation ones, which need
// write.
func TestPromptsVisibleWhenEveryDrivenToolIs(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:*"))

	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsImpactful,
		func(o *Options) { o.ACL = evaluator },
	)

	got := promptNames(listPromptsAs(t, srv, &auth.Identity{Subject: "tester"}))

	found := make(map[string]bool, len(got))
	for _, name := range got {
		found[name] = true
	}

	if !found["diagnose_service"] {
		t.Error("service:read must reveal diagnose_service")
	}

	if found["roll_back_service"] {
		t.Error("service:read must not reveal roll_back_service, which writes")
	}
}

// TestPromptsHiddenEntirelyWithoutGrants is the floor. A caller matching no
// grant can read nothing, so there is no investigation to seed — even though
// the cross-type read tools stay visible in tools/list on their own reasoning.
func TestPromptsHiddenEntirelyWithoutGrants(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:*"},
		Audience:    []string{"user:somebody-else"},
		Permissions: []string{"read"},
	}}})

	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsImpactful,
		func(o *Options) { o.ACL = evaluator },
	)

	if got := promptNames(listPromptsAs(t, srv, &auth.Identity{Subject: "tester"})); len(got) != 0 {
		t.Errorf("a zero-grant caller was offered %v, want nothing", got)
	}
}

// TestPromptsGetOnAHiddenPromptSaysNotFound is a disclosure property. mcp-go
// enforces the filter at get time too and returns the same error as an unknown
// name, so a hidden prompt cannot be confirmed by guessing it. Inherited rather
// than built, and pinned because it is security-relevant.
func TestPromptsGetOnAHiddenPromptSaysNotFound(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("node:*"))

	srv := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsImpactful,
		func(o *Options) { o.ACL = evaluator },
	)

	request := modernRequest(t, 1, "prompts/get",
		`{"name":"diagnose_service","arguments":{"service":"web"}}`)
	request = request.WithContext(
		auth.ContextWithIdentity(request.Context(), &auth.Identity{Subject: "tester"}),
	)

	_, envelope := sendMCP(t, srv.Handler(), request)
	if envelope.Error == nil {
		t.Fatal("prompts/get returned a prompt the caller may not see")
	}

	if !strings.Contains(envelope.Error.Message, "not found") {
		t.Errorf("error was %q; a hidden prompt must be indistinguishable from an unknown one",
			envelope.Error.Message)
	}
}
```

> The two remediation prompt names (`drain_node`, `roll_back_service`) appear here before Task 5 adds them. That is deliberate: the assertions are "must NOT be offered", which hold vacuously until the prompts exist and then keep holding. Only `TestPromptsVisibleWhenEveryDrivenToolIs` reads a positive assertion on a Task-2 prompt.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run "TestPromptsHidden|TestPromptsVisible|TestPromptsGetOnAHidden" 2>&1 | head -20`
Expected: FAIL — every prompt is listed regardless of identity, so `diagnose_service` is offered to a node-only caller and the zero-grant caller sees three prompts.

- [ ] **Step 3: Write the filter**

In `internal/mcp/server.go`, immediately after `filterToolsForIdentity`:

```go
// filterPromptsForIdentity is wired as a WithPromptFilter for prompts/list.
// mcp-go applies it at prompts/get as well, returning the same "not found" as
// an unknown name, so a hidden prompt cannot be confirmed by guessing it.
//
// A prompt is a sequence, so visibility is all-or-nothing: if the caller
// cannot perform one step, the runbook dead-ends partway — and for a
// remediation prompt, possibly after the model has already written. One
// unavailable tool hides the whole prompt.
func (s *Server) filterPromptsForIdentity(
	ctx context.Context,
	prompts []mcplib.Prompt,
) []mcplib.Prompt {
	visibility := s.toolVisibilityFor(ctx)

	// A caller matching no grant can read nothing, so there is no
	// investigation to seed. The cross-type read tools stay visible in
	// tools/list on their own reasoning — each ACL-filters its own results —
	// but a prompt promising a sequence over them promises nothing.
	if visibility.noGrants {
		return nil
	}

	if visibility.allow == nil {
		return prompts
	}

	drives := make(map[string][]string, len(s.registeredPrompts))
	for _, def := range s.registeredPrompts {
		drives[def.prompt.Name] = def.drives
	}

	out := make([]mcplib.Prompt, 0, len(prompts))
	for _, prompt := range prompts {
		if allToolsVisible(drives[prompt.Name], visibility.allow) {
			out = append(out, prompt)
		}
	}

	return out
}

// allToolsVisible reports whether every named tool passes allow.
func allToolsVisible(tools []string, allow func(name string) bool) bool {
	for _, name := range tools {
		if !allow(name) {
			return false
		}
	}

	return true
}
```

- [ ] **Step 4: Wire the filter**

In `serverOptions`, beside `WithPromptCapabilities`:

```go
		mcpserver.WithPromptFilter(srv.filterPromptsForIdentity),
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -count=1 2>&1 | tail -5`
Expected: PASS, whole package.

- [ ] **Step 6: Verify under race**

Run: `go test ./internal/mcp/ -race -count=1 2>&1 | tail -3`
Expected: PASS. The filter runs per request against `registeredPrompts`, which is written once during `New` and only read afterwards.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/mcp/server.go internal/mcp/prompts_e2e_test.go
golangci-lint run ./internal/...
git add internal/mcp/server.go internal/mcp/prompts_e2e_test.go
git commit -m "feat(mcp): hide prompts a caller could not complete

Visibility derives from the tools a prompt drives, all-or-nothing: a sequence
whose step 4 the caller cannot perform dead-ends partway, and for a remediation
prompt possibly after the model has already written.

A caller matching no grant is offered nothing, which is the one case where
'every driven tool is visible' fails to imply 'the prompt is useful' — the
cross-type reads stay visible on their own reasoning, but a sequence over them
promises nothing."
```

**Batch A exit criteria.** `prompts/list` and `prompts/get` work; three diagnostic prompts, tier-gated and ACL-filtered; whole package green under `-race`. Shippable on its own — if it ships without Batch B, do Task 6's documentation steps for the three prompts that exist.

---

### Task 5: The three remediation prompts

Pure catalog and text additions against a mechanism already proven, and the task where tier gating stops being theoretical: one prompt per tier above 0.

**Files:**
- Modify: `internal/mcp/prompt_text.go`
- Modify: `internal/mcp/prompts.go` (`promptCatalog`)
- Test: `internal/mcp/prompts_test.go`, `internal/mcp/prompts_e2e_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2–4.
- Produces: text constants `promptTextRollBackService`, `promptTextRightSizeService`, `promptTextDrainNode`; three new `promptCatalog` entries.

- [ ] **Step 1: Write the failing test**

In `internal/mcp/prompts_test.go`, extend the two `want` maps:

```go
	// in TestPromptTierIsTheMaxOfItsTools
	want := map[string]config.OperationsLevel{
		"diagnose_service":      config.OpsReadOnly,
		"explain_unschedulable": config.OpsReadOnly,
		"review_capacity":       config.OpsReadOnly,
		"roll_back_service":     config.OpsOperational,
		"right_size_service":    config.OpsConfiguration,
		"drain_node":            config.OpsImpactful,
	}

	// in TestPromptCatalogDeclaresItsArguments
	want := map[string]string{
		"diagnose_service":      "service",
		"explain_unschedulable": "service",
		"review_capacity":       "",
		"roll_back_service":     "service",
		"right_size_service":    "service",
		"drain_node":            "node",
	}
```

Append to `internal/mcp/prompts_test.go`:

```go
// TestRemediationPromptsConfirmBeforeActing is rule 3 of the text
// conventions, and the reason remediation prompts are safe to ship: a runbook
// that assumes its own diagnosis turns "roll this back" into an outage when
// the service was healthy all along.
func TestRemediationPromptsConfirmBeforeActing(t *testing.T) {
	for name, mutation := range map[string]string{
		"roll_back_service":  "rollback_service",
		"right_size_service": "update_service_resources",
		"drain_node":         "update_node_availability",
	} {
		def := findPrompt(t, name)

		result, err := def.handler(context.Background(), getPromptRequest(
			name,
			map[string]string{"service": "example", "node": "example"},
		))
		if err != nil {
			t.Fatalf("prompt %q handler: %v", name, err)
		}

		text, ok := result.Messages[0].Content.(mcplib.TextContent)
		if !ok {
			t.Fatalf("prompt %q content is %T", name, result.Messages[0].Content)
		}

		if !strings.Contains(text.Text, mutation) {
			t.Errorf("prompt %q never names the %q tool it exists to drive", name, mutation)
		}

		// The text must offer an exit that makes no change. Without one, the
		// model has no instruction for "the problem is not real".
		if !strings.Contains(text.Text, "stop") && !strings.Contains(text.Text, "no change") {
			t.Errorf("prompt %q gives the model no way to stop without acting", name)
		}
	}
}
```

Append to `internal/mcp/prompts_e2e_test.go`:

```go
// TestPromptsListAtEachTier is what makes the gating observable: every tier
// above 0 adds exactly one remediation prompt.
func TestPromptsListAtEachTier(t *testing.T) {
	for _, testCase := range []struct {
		level config.OperationsLevel
		want  []string
	}{
		{
			level: config.OpsReadOnly,
			want:  []string{"diagnose_service", "explain_unschedulable", "review_capacity"},
		},
		{
			level: config.OpsOperational,
			want: []string{
				"diagnose_service", "explain_unschedulable", "review_capacity",
				"roll_back_service",
			},
		},
		{
			level: config.OpsConfiguration,
			want: []string{
				"diagnose_service", "explain_unschedulable", "review_capacity",
				"roll_back_service", "right_size_service",
			},
		},
		{
			level: config.OpsImpactful,
			want: []string{
				"diagnose_service", "explain_unschedulable", "review_capacity",
				"roll_back_service", "right_size_service", "drain_node",
			},
		},
	} {
		handler := newPromptTestServer(t, testCase.level).Handler()

		got := promptNames(listPrompts(t, handler))
		if len(got) != len(testCase.want) {
			t.Errorf("tier %v listed %v, want %v", testCase.level, got, testCase.want)

			continue
		}

		listed := make(map[string]bool, len(got))
		for _, name := range got {
			listed[name] = true
		}

		for _, name := range testCase.want {
			if !listed[name] {
				t.Errorf("tier %v did not list %q", testCase.level, name)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run "TestPromptTier|TestRemediationPrompts|TestPromptsListAtEachTier|TestPromptCatalogDeclares" 2>&1 | head -20`
Expected: FAIL — `findPrompt` calls `t.Fatalf` with `prompt "roll_back_service" is not in promptCatalog()`, and the tier listings are short by one prompt each.

- [ ] **Step 3: Write the remediation text**

Append to the `const` block in `internal/mcp/prompt_text.go`:

```go
	promptTextRollBackService = `You have been asked to roll %s back to its previous version. Confirm the
rollback is warranted before performing it.

1. Resolve the name with the search tool, then read cetacean://services/<id>.
   Check UpdateStatus: a service already rolling_back needs no second rollback,
   and one still updating may converge on its own.
2. Call list_resources for tasks and confirm this service actually has failing
   or unplaced tasks. If every task is running and healthy, stop and report
   that - there is nothing to roll back, and rolling back a healthy service is
   an outage you caused.
3. Read cetacean://history to confirm what changed and when. A rollback undoes
   the last spec update, which may not be the change that broke it.
4. Call rollback_service with task.ttl set, so the call reports when the
   previous version's replicas are actually running rather than when Docker
   accepted the request.
5. Poll tasks/get until the task settles. On failure read statusMessage: a
   rollback can fail for the same reason the update did.

Report the version rolled back to and the running replica count afterwards. If
step 2 showed a healthy service, report that and make no change.`

	promptTextRightSizeService = `Right-size the resource reservations for %s, using measured use rather than a
guess.

1. Call get_recommendations and find the sizing finding for this service.
   Cetacean computes it from observed use over the configured lookback window.
   If there is no finding, the service is already within thresholds - say so
   and stop.
2. Resolve the name with the search tool, then read cetacean://services/<id>
   for its current Resources.Reservations and Resources.Limits.
3. Call get_metrics for cpu and memory over the last week and check the finding
   against the series yourself. A service whose load is weekly or bursty can
   look over-provisioned over a day and be correctly sized over a week.
4. Apply the change with update_service_resources. Raise reservations before
   lowering limits, and never set a reservation above a limit.
5. Confirm the tasks rescheduled and are running. Changing reservations
   replaces every task, so this is a rolling restart, not a metadata edit.

Report the old and new values and the evidence for them. If the metric window
disagrees with the recommendation, report the disagreement and change nothing.`

	promptTextDrainNode = `Drain node %s for maintenance, without moving work somewhere it cannot run.

1. Resolve the name with the search tool, then read cetacean://nodes/<id> for
   its role, current availability and resources.
2. If this node is a manager, read cetacean://cluster for the manager count
   first. Draining a manager does not remove it from the raft quorum, but
   losing it while drained does - and a two-manager quorum has no tolerance at
   all. Report and stop if draining would leave fewer than three managers,
   unless you have been told otherwise.
3. Call list_resources for tasks and list every task currently on this node,
   with its service. This is the work that has to land elsewhere.
4. Call list_resources for nodes and check that the remaining active nodes
   satisfy each of those services' placement constraints and have the
   reservations spare. A service constrained to this node alone has nowhere to
   go and will sit pending.
5. Only then call update_node_availability with drain.
6. Poll the affected services' tasks until they are running elsewhere. Report
   any that stayed pending, with the constraint that blocked them.

Report what moved, where it moved to, and anything that could not move. If step
2 or 4 found a blocker, report it and leave the node active.`
```

- [ ] **Step 4: Add the catalog entries**

Append these three entries to the slice returned by `promptCatalog()` in `internal/mcp/prompts.go`:

```go
		{
			prompt: mcplib.NewPrompt("roll_back_service",
				mcplib.WithPromptTitle("Roll a service back"),
				mcplib.WithPromptDescription(
					"Confirm a service is actually degraded, then roll it back to its "+
						"previous version and wait for the replicas to run.",
				),
				mcplib.WithArgument("service",
					mcplib.ArgumentTitle("Service"),
					mcplib.ArgumentDescription("Name or ID of the service to roll back"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{"search", "list_resources", "rollback_service"},
			handler: interpolatingHandler(
				"Roll a service back",
				promptTextRollBackService,
				"service",
			),
		},
		{
			prompt: mcplib.NewPrompt("right_size_service",
				mcplib.WithPromptTitle("Right-size a service"),
				mcplib.WithPromptDescription(
					"Check a sizing recommendation against measured use, then correct the "+
						"service's reservations. Replaces every task.",
				),
				mcplib.WithArgument("service",
					mcplib.ArgumentTitle("Service"),
					mcplib.ArgumentDescription("Name or ID of the service to right-size"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{
				"search",
				"get_metrics",
				"get_recommendations",
				"update_service_resources",
			},
			handler: interpolatingHandler(
				"Right-size a service",
				promptTextRightSizeService,
				"service",
			),
		},
		{
			prompt: mcplib.NewPrompt("drain_node",
				mcplib.WithPromptTitle("Drain a node for maintenance"),
				mcplib.WithPromptDescription(
					"Check quorum and that the node's work can be placed elsewhere, then "+
						"drain it and confirm the tasks moved.",
				),
				mcplib.WithArgument("node",
					mcplib.ArgumentTitle("Node"),
					mcplib.ArgumentDescription("Hostname or ID of the node to drain"),
					mcplib.RequiredArgument(),
				),
			),
			drives: []string{"search", "list_resources", "update_node_availability"},
			handler: interpolatingHandler(
				"Drain a node for maintenance",
				promptTextDrainNode,
				"node",
			),
		},
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -count=1 2>&1 | tail -5`
Expected: PASS, whole package. `TestPromptCatalogDrivesOnlyRealTools` now covers the three mutation tools as well, and the ACL tests written in Task 4 that asserted `drain_node` and `roll_back_service` are *not* offered stop holding vacuously.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/mcp/prompts.go internal/mcp/prompt_text.go internal/mcp/prompts_test.go internal/mcp/prompts_e2e_test.go
golangci-lint run ./internal/...
git add internal/mcp/prompts.go internal/mcp/prompt_text.go internal/mcp/prompts_test.go internal/mcp/prompts_e2e_test.go
git commit -m "feat(mcp): add the three remediation prompts

One per operations tier above 0, so the gating is observable rather than
theoretical: a tier-0 deployment offers only the diagnostic prompts and each
higher tier adds exactly one.

Each opens by confirming the problem is real and offers an exit that changes
nothing, which is what makes shipping them safe — a runbook that assumes its own
diagnosis turns 'roll this back' into an outage when the service was healthy."
```

---

### Task 6: Documentation

**Files:**
- Modify: `docs/mcp.md` (new Prompts section, after the Tools section around line 248)
- Modify: `CLAUDE.md` (the `mcp/` architecture bullet)
- Modify: `CHANGELOG.md` (under `[Unreleased]` → `### Added`)
- Modify: `docs/specs/2026-09-04-mcp-prompts-design.md` (status line)

- [ ] **Step 1: Add the docs/mcp.md section**

Insert after the Tools section, before Configuration:

```markdown
## Prompts

Prompts are named sequences a client offers from a menu: picking one seeds the
conversation with an investigation or a runbook, so an agent does not have to
rediscover which order of calls answers a question.

| Prompt | Tier | Argument | What it does |
|---|---|---|---|
| `diagnose_service` | 0 | `service` | Walks tasks, the failing task's logs, metrics and recent changes to find why a service is unhealthy |
| `explain_unschedulable` | 0 | `service` | Separates the four causes of an unplaced task: placement constraints, node labels, node availability, reservations |
| `review_capacity` | 0 | — | Joins node capacity, reservations, real usage and sizing findings to say where the cluster is constrained |
| `roll_back_service` | 1 | `service` | Confirms a service is actually degraded, then rolls it back and waits for the replicas to run |
| `right_size_service` | 2 | `service` | Checks a sizing recommendation against measured use, then corrects the reservations |
| `drain_node` | 3 | `node` | Checks quorum and that the work can be placed elsewhere, then drains and confirms the tasks moved |

A prompt's tier is the highest tier of the tools it walks, so it is never
offered where one of its steps would be refused. `CETACEAN_MCP_OPERATIONS_LEVEL`
therefore controls prompts as it controls tools: at the default tier 1 you get
the three diagnostic prompts plus `roll_back_service`.

Prompts are also filtered by ACL, all-or-nothing: a prompt is offered only when
**every** tool it walks is available to you. A sequence whose fourth step you
cannot perform would dead-end partway, and for a remediation prompt possibly
after a write. A caller whose grants match nothing is offered no prompts at all.
A prompt you cannot see reports `not found` from `prompts/get`, the same as a
name that does not exist.

Prompts read no cluster data. They expand to a single message with the resource
name you supplied interpolated, and the name is not checked for existence — the
text tells the model to resolve it with `search` first. A prompt is a plan for
the model to carry out, not a report; every read and write it describes still
goes through the ordinary tool and resource paths, with the ordinary ACL checks.
```

- [ ] **Step 2: Update the CLAUDE.md architecture note**

In the `mcp/` bullet, after the sentence describing tools and resources, add:

```
`internal/mcp/prompts.go` holds the prompt catalog: six named investigation and remediation sequences. A `promptDef` declares the tools it `drives`, and **both** its operations tier (`promptTier` — the max tier of those tools) and its per-caller visibility (`filterPromptsForIdentity` — every driven tool must be visible) derive from that one declaration, so a prompt cannot advertise itself below the tier of a mutation it instructs. `toolVisibilityFor` is the predicate shared with `filterToolsForIdentity`; its `noGrants` flag exists for prompts, which are hidden entirely from a caller matching no grant even though the ungated cross-type reads stay visible in `tools/list`. `WithPromptCapabilities` is separate from `AddPrompt` — registering prompts without it serves none of them, so `TestPromptsCapabilityIsWired` drives the real transport. Prompt text lives in `prompt_text.go` as raw-string constants (hence no backticks) under four rules, three test-enforced: name every driven tool, make no claim about *this* cluster, and give a remediation prompt an exit that changes nothing.
```

- [ ] **Step 3: Add the CHANGELOG entry**

Under `[Unreleased]` → `### Added`:

```markdown
- AI agent clients can now offer Cetacean's investigations by name instead of a flat list of tools: diagnose an unhealthy service, explain why one will not schedule, review cluster capacity, roll a service back, right-size it, or drain a node for maintenance. Each seeds the agent with the order to work in — which is where the Swarm knowledge lives, such as checking that a drained node's work can actually be placed elsewhere before draining it. Which ones you are offered follows the operations level and your own permissions: you only see a sequence you could carry out from end to end, and the remediation ones each start by confirming the problem is real before changing anything
```

- [ ] **Step 4: Flip the design doc status**

In `docs/specs/2026-09-04-mcp-prompts-design.md`, change `**Status:** designed` to `**Status:** implemented`.

- [ ] **Step 5: Verify everything**

```bash
go build ./... && go test ./... 2>&1 | grep -v "^ok\|no test files"
go test ./internal/mcp/ -race -count=1 2>&1 | tail -3
golangci-lint run ./... 2>&1 | tail -3
```
Expected: no failures, `0 issues`.

- [ ] **Step 6: Commit**

```bash
git add docs/mcp.md CLAUDE.md CHANGELOG.md docs/specs/2026-09-04-mcp-prompts-design.md
git commit -m "docs: document the MCP prompts

Records the rule that matters to an operator: a prompt is offered only when
every tool it walks is available, so the operations level and ACL control
prompts exactly as they control tools, and a caller is never handed a sequence
that dead-ends partway."
```

---

## Self-Review

**Spec coverage.** Walked each spec section against the tasks:

| Spec section | Task |
|---|---|
| Library findings (filter exists, enforced at get, capability separate) | 3 (capability), 4 (get-time disclosure test) |
| Prompts may remediate | 5 |
| Declares tools; tier and ACL derive | 2 (tier), 4 (ACL) |
| Visibility all-or-nothing | 4 (`allToolsVisible`) |
| Coarse visibility + zero-grant floor | 4 (`noGrants`) |
| Handlers read no cluster data | 2 (pure `interpolatingHandler`/`staticHandler`) |
| Text is reviewed; five rules | 2 (rules 1, 4, 5 + no-backtick test), 5 (rule 3 test) |
| `toolVisibilityFor` extraction | 1 |
| Registration and capability | 3 |
| The six prompts and their text | 2, 5 |
| Completions seam | 2 (argument names asserted as the contract) |
| Testing section, all five bullets | 2 (wording, drift), 3 (tier, capability), 4 (ACL both directions, disclosure) |
| Documentation | 6 |

No gaps. Rule 2 of the text conventions ("state the stopping condition") is enforced only for remediation prompts, where acting wrongly is expensive; asserting a stopping phrase in the diagnostic text would pin wording without protecting anything.

**Placeholder scan.** No TBDs. Every code step carries the actual code, using
the real helpers (`strings.Contains`, `newResourceTestServer`'s variadic
overrides, `env.Error.Message`) rather than stand-ins — each verified present in
the tree before this plan was written.

**Type consistency.** `toolVisibility{allow, noGrants}` is produced in Task 1 and consumed in Tasks 1 and 4 with those field names. `promptDef{prompt, drives, handler}` is produced in Task 2 and consumed in Tasks 3, 4, 5. `promptTier(def, tiers)` and `s.toolTiers()` keep their Task 2 signatures in Tasks 3 and 5. `requiredArgument(req, name)` is used only inside `interpolatingHandler`. Test helpers `findPrompt`, `getPromptRequest`, `listPrompts`, `promptNames`, `listPromptsAs`, `newPromptTestServer` are each defined once, in the task that first uses them.

**Ordering.** Task 4's tests name `drain_node` and `roll_back_service` before Task 5 creates them; the assertions are negative and hold vacuously until then, which is noted in the task.
