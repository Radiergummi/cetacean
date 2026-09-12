# MCP Write Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the gap where Cetacean diagnoses problems it cannot treat — an agent is told six services have no health check and has no tool to add one — by exposing the service-spec writes that already exist behind REST.

**Architecture:** Almost nothing new is built. `internal/docker/client.go` already implements every writer this needs (`UpdateServiceHealthcheck`, `UpdateServiceContainerConfig`, `UpdateServiceConfigs`, `UpdateServiceSecrets`, `UpdateServiceMounts`, `CreateSecret`, `CreateConfig`) and `internal/api` already routes to all of them. The work is MCP wiring: two new sections on the existing `update_service`, and five new tools — separate tools rather than more sections because a tool is the unit of visibility and these target different resource types.

**Tech Stack:** Go 1.26, `mark3labs/mcp-go` v1.0.0, Docker Engine API types.

**Spec:** `docs/superpowers/specs/2026-09-04-mcp-agent-surface-design.md` (plan 3 of 3; plan 1 shipped as `b46730c8` and predecessors, plan 2 is `2026-09-05-mcp-read-verbs.md`)

## Global Constraints

- **A tool is the unit of visibility.** `tools/list` is filtered by operations tier *and* by `toolVisibilityFor`'s per-type ACL check. If a caller can see a tool, every part of it must be callable. Collapse only within one tier **and** one target resource type. Never compute a tier at call time.
- **A write answers with the section it changed, never the whole Docker object.** `65553bfd` fixed a leak where every spec editor returned the full `swarm.Service`, handing back env values and log-driver options to a caller that only raised a CPU limit. New sections must extend `serviceSectionKeys`, not bypass it.
- **Env variable names only, never values.** **Secret data stays zeroed.**
- **Units are named, never implied.** Durations are strings (`"10s"`).
- **Nil slices must not marshal to `null`.**
- **Every tool declares `WithOutputSchema`.**
- All new exported types and functions carry doc comments saying *why*, not *what*.

## Two decisions this plan makes explicit

**1. Shape.** The spec proposed `update_service_config(id, changes)` taking a bag of changes. What shipped in `b46730c8` is `update_service(id, section, value)`, naming one section per call. This plan follows **what shipped** — `healthcheck` and `command` become two more sections — because the section shape is already documented, already tested, and already what every client has learned. Re-cutting it now would be churn for no gain.

**2. Tier 2, matching REST — overriding the spec.** The spec put secrets, configs and mounts at **tier 3** for MCP, reasoning that changing which credentials a container receives, or binding `/var/run/docker.sock` into it, is a privilege-escalation path an agent should be held to a higher bar for.

**That was considered and rejected.** REST routes the same three operations at tier 2 (`internal/api/router.go:376,381,391`), and splitting them would mean an operator at `CETACEAN_OPERATIONS_LEVEL=2` sees those editors in the dashboard but not in `tools/list` — a surprise with no signal explaining it, in a product whose whole authorization story is "MCP is a second transport over the existing model, not a new privilege path" (`docs/mcp.md`). Consistency between transports wins; the operations level is the operator's single dial, and it should mean one thing.

So **all three sit at `config.OpsConfiguration` (tier 2)**, and there is no divergence to document. The danger of a host bind mount does not go away — it is stated in `update_service_mounts`' description, where the model reads it, which is the right place for it rather than in a tier the operator did not ask for.

## File Structure

| File | Responsibility |
|---|---|
| `internal/mcp/updates.go` (modify) | Add `healthcheck` and `command` to the section constants, the `serviceSectionKeys` projection table, and the `update_service` dispatch. |
| `internal/mcp/creates.go` (create) | `create_secret` and `create_config`. |
| `internal/mcp/attachments.go` (create) | `update_service_secrets`, `update_service_configs`, `update_service_mounts` — the three attachment editors, all tier 2. |
| `internal/mcp/tools.go` (modify) | Extend the `ServiceSpecWriter` interface; add the new tools to `toolCatalog()`. |
| `internal/mcp/server.go` (modify) | `toolACLSpecs` entries for the new tools. |
| `internal/cluster/view.go` (modify) | `ServiceDetails` gains `healthcheck` detail, `command`, `mountTargets`, `secretNames`, `configNames` so a write can answer with the section it changed. |
| `docs/mcp.md`, `CHANGELOG.md` (modify) | Documentation. |

---

### Task 1: `healthcheck` and `command` sections

**Files:**
- Modify: `internal/cluster/view.go` (`ServiceDetails`), `internal/mcp/updates.go`, `internal/mcp/tools.go`
- Test: `internal/cluster/view_test.go`, `internal/mcp/updates_test.go`

**Interfaces:**
- Consumes: `docker.Client.UpdateServiceHealthcheck(ctx, id string, hc *container.HealthConfig) (swarm.Service, error)`, `docker.Client.UpdateServiceContainerConfig(ctx, id string, ...) (swarm.Service, error)` — read their exact signatures at `internal/docker/client.go:770` and `:1089` before writing the interface additions.
- Produces: section constants `sectionHealthcheck = "healthcheck"`, `sectionCommand = "command"`; entries in `serviceSectionKeys`.

`get_recommendations` reports "Service has no health check configured" for six of eight services on the evaluation cluster, and there is no tool to act on it. This is the sharpest instance of the diagnose-without-treat gap.

- [ ] **Step 1: Write the failing test for the details projection**

`ServiceDetails` currently reports `healthcheck` as a bare bool. A write that sets one must be able to answer with what it set, so the projection needs the detail.

```go
// in internal/cluster/view_test.go

// A write answers with the section it changed, and for a healthcheck the
// interesting part is the test command and the timings — a bool cannot confirm
// what was just configured.
func TestServiceDetailsReportsHealthcheckDetail(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Healthcheck: &container.HealthConfig{
						Test:     []string{"CMD", "curl", "-f", "http://localhost/"},
						Interval: 10 * time.Second,
						Timeout:  3 * time.Second,
						Retries:  3,
					},
				},
			},
		},
	}

	got := ServiceDetails(svc)

	if got["healthcheck"] != true {
		t.Errorf("healthcheck = %v, want true", got["healthcheck"])
	}
	if got["healthcheckInterval"] != "10s" {
		t.Errorf("healthcheckInterval = %v, want \"10s\" (a duration string, not a nanosecond integer)", got["healthcheckInterval"])
	}
	if got["healthcheckTimeout"] != "3s" {
		t.Errorf("healthcheckTimeout = %v, want \"3s\"", got["healthcheckTimeout"])
	}
	if got["healthcheckRetries"] != 3 {
		t.Errorf("healthcheckRetries = %v, want 3", got["healthcheckRetries"])
	}
}

// The command is what a caller edits and what confirms the edit.
func TestServiceDetailsReportsCommand(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Command: []string{"sh", "-c"},
					Args:    []string{"echo hi"},
				},
			},
		},
	}

	got := ServiceDetails(svc)

	cmd, ok := got["command"].([]string)
	if !ok || len(cmd) != 2 {
		t.Fatalf("command = %v, want the entrypoint slice", got["command"])
	}
	args, ok := got["args"].([]string)
	if !ok || len(args) != 1 {
		t.Fatalf("args = %v, want the argument slice", got["args"])
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cluster/ -run 'TestServiceDetailsReportsHealthcheck|TestServiceDetailsReportsCommand'`
Expected: FAIL — the keys are absent.

- [ ] **Step 3: Extend `ServiceDetails`**

Find `ServiceDetails` in `internal/cluster/view.go`. It already sets `healthcheck` as a bool. Extend that branch:

```go
	if hc := containerSpec(svc).Healthcheck; hc != nil {
		details["healthcheck"] = true

		// Named as durations rather than the nanosecond integers Docker's own
		// type carries, which read as arbitrary large numbers.
		if hc.Interval > 0 {
			details["healthcheckInterval"] = hc.Interval.String()
		}
		if hc.Timeout > 0 {
			details["healthcheckTimeout"] = hc.Timeout.String()
		}
		if hc.StartPeriod > 0 {
			details["healthcheckStartPeriod"] = hc.StartPeriod.String()
		}
		if hc.Retries > 0 {
			details["healthcheckRetries"] = hc.Retries
		}
		if len(hc.Test) > 0 {
			details["healthcheckTest"] = hc.Test
		}
	} else {
		details["healthcheck"] = false
	}

	if cmd := containerSpec(svc).Command; len(cmd) > 0 {
		details["command"] = cmd
	}
	if args := containerSpec(svc).Args; len(args) > 0 {
		details["args"] = args
	}
```

**Note:** `containerSpec(svc)` is shorthand — check whether `internal/cluster/view.go` already has such a helper. If it does not, guard the nil `ContainerSpec` inline; `svc.Spec.TaskTemplate.ContainerSpec` is a pointer and is nil for a service with no container spec.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cluster/`
Expected: PASS. Existing golden/digest tests may need their expected maps widened — that is correct, not a regression.

- [ ] **Step 5: Write the failing MCP test**

```go
// in internal/mcp/updates_test.go

// The gap this task closes: get_recommendations reports six services with no
// health check and, until now, nothing could add one.
func TestUpdateServiceSetsAHealthcheck(t *testing.T) {
	c, srv, writes := newUpdateTestServer(t)
	seedService(t, c, "svc1", "web")

	body, err := srv.toolUpdateService(context.Background(), toolRequest(t, "update_service", map[string]any{
		"id":      "web",
		"section": "healthcheck",
		"value": map[string]any{
			"test":     []any{"CMD", "curl", "-f", "http://localhost/"},
			"interval": "10s",
			"timeout":  "3s",
			"retries":  float64(3),
		},
	}))
	if err != nil {
		t.Fatalf("toolUpdateService: %v", err)
	}

	if writes.healthcheck == nil {
		t.Fatal("no healthcheck reached the Docker client")
	}
	if writes.healthcheck.Interval != 10*time.Second {
		t.Errorf("interval = %v, want 10s", writes.healthcheck.Interval)
	}

	// The reply must confirm the section, and nothing else.
	var got serviceUpdateResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Details["healthcheckInterval"] != "10s" {
		t.Errorf("details = %+v, want the healthcheck it just set", got.Details)
	}
	if _, leaked := got.Details["envNames"]; leaked {
		t.Error("the reply carries fields outside the edited section")
	}
}

// A duration a model is likely to guess wrong must be rejected by name.
func TestUpdateServiceRejectsANonDurationInterval(t *testing.T) {
	c, srv, _ := newUpdateTestServer(t)
	seedService(t, c, "svc1", "web")

	_, err := srv.toolUpdateService(context.Background(), toolRequest(t, "update_service", map[string]any{
		"id":      "web",
		"section": "healthcheck",
		"value":   map[string]any{"test": []any{"CMD", "true"}, "interval": "ten seconds"},
	}))
	if err == nil {
		t.Fatal("expected an error naming the duration format")
	}
	if !strings.Contains(err.Error(), "interval") {
		t.Errorf("error does not name the offending field: %v", err)
	}
}

func TestUpdateServiceSetsTheCommand(t *testing.T) {
	c, srv, writes := newUpdateTestServer(t)
	seedService(t, c, "svc1", "web")

	_, err := srv.toolUpdateService(context.Background(), toolRequest(t, "update_service", map[string]any{
		"id":      "web",
		"section": "command",
		"value":   map[string]any{"command": []any{"sh", "-c"}, "args": []any{"echo hi"}},
	}))
	if err != nil {
		t.Fatalf("toolUpdateService: %v", err)
	}

	if len(writes.command) != 2 || writes.command[0] != "sh" {
		t.Errorf("command = %v, want [sh -c]", writes.command)
	}
}
```

**Note:** `newUpdateTestServer`, `seedService` and the fake write client are existing helpers in `internal/mcp/updates_test.go` or `tools_test.go`. Read them first and extend the fake with `healthcheck *container.HealthConfig` and `command []string` capture fields rather than writing a second fake.

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/mcp/ -run 'TestUpdateServiceSetsAHealthcheck|TestUpdateServiceRejects|TestUpdateServiceSetsTheCommand'`
Expected: FAIL — `section "healthcheck"` is not in the enum.

- [ ] **Step 7: Add the sections**

In `internal/mcp/updates.go`, add the constants:

```go
	sectionHealthcheck = "healthcheck"
	sectionCommand     = "command"
```

Add to `serviceSectionKeys`:

```go
	sectionHealthcheck: {
		"healthcheck",
		"healthcheckTest",
		"healthcheckInterval",
		"healthcheckTimeout",
		"healthcheckStartPeriod",
		"healthcheckRetries",
	},
	sectionCommand: {"command", "args"},
```

Add the decode types and dispatch branches. The healthcheck value takes durations as strings, per the units rule:

```go
// healthcheckValue is the healthcheck section's argument.
//
// Durations arrive as strings ("10s") rather than the nanosecond integers
// Docker's own container.HealthConfig carries, because a model writing
// 10000000000 is a model that has already made a mistake. Parse errors name
// the field, since a caller that guessed the format has to be told which of
// four it got wrong.
type healthcheckValue struct {
	Test        []string `json:"test"`
	Interval    string   `json:"interval,omitempty"`
	Timeout     string   `json:"timeout,omitempty"`
	StartPeriod string   `json:"startPeriod,omitempty"`
	Retries     int      `json:"retries,omitempty"`
}

// toHealthConfig converts the wire shape, or names the field that would not
// parse. A nil result with a nil error means "remove the healthcheck".
func (v healthcheckValue) toHealthConfig() (*container.HealthConfig, error) {
	if len(v.Test) == 0 {
		return nil, nil
	}

	hc := &container.HealthConfig{Test: v.Test, Retries: v.Retries}

	for _, field := range []struct {
		name  string
		raw   string
		into  *time.Duration
	}{
		{"interval", v.Interval, &hc.Interval},
		{"timeout", v.Timeout, &hc.Timeout},
		{"startPeriod", v.StartPeriod, &hc.StartPeriod},
	} {
		if field.raw == "" {
			continue
		}

		parsed, err := time.ParseDuration(field.raw)
		if err != nil {
			return nil, fmt.Errorf(
				"healthcheck %s: %q is not a duration (e.g. \"10s\", \"1m30s\")",
				field.name, field.raw,
			)
		}

		*field.into = parsed
	}

	return hc, nil
}

// commandValue is the command section's argument. Command is the entrypoint,
// Args what follows it — the same split swarm.ContainerSpec makes.
type commandValue struct {
	Command []string `json:"command"`
	Args    []string `json:"args,omitempty"`
}
```

In `toolUpdateService`'s section switch, add:

```go
	case sectionHealthcheck:
		value, err := decodeSection[healthcheckValue](req, sectionHealthcheck)
		if err != nil {
			return "", err
		}

		hc, err := value.toHealthConfig()
		if err != nil {
			return "", err
		}

		updated, err = s.writes.UpdateServiceHealthcheck(ctx, svc.ID, hc)

	case sectionCommand:
		value, err := decodeSection[commandValue](req, sectionCommand)
		if err != nil {
			return "", err
		}

		updated, err = s.writes.UpdateServiceCommand(ctx, svc.ID, value.Command, value.Args)
```

**Note:** `UpdateServiceCommand` may not exist under that name — `internal/docker/client.go:1089` has `UpdateServiceContainerConfig`, which likely also carries user and working directory. Read its signature and either call it with the other fields left unchanged, or add a narrow `UpdateServiceCommand` beside it. Prefer calling the existing one: this plan adds no Docker plumbing.

Add both methods to the `ServiceSpecWriter` interface in `internal/mcp/tools.go`.

Finally, extend the `section` enum in the `update_service` tool declaration and its description to name the two new sections and say that healthcheck durations are strings.

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./internal/mcp/ ./internal/cluster/`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/cluster/view.go internal/cluster/view_test.go internal/mcp/updates.go internal/mcp/updates_test.go internal/mcp/tools.go
git commit -m "feat(mcp): add healthcheck and command sections to update_service"
```

---

### Task 2: `create_secret` and `create_config`

**Files:**
- Create: `internal/mcp/creates.go`
- Modify: `internal/mcp/tools.go`, `internal/mcp/server.go`
- Test: `internal/mcp/creates_test.go`

**Interfaces:**
- Consumes: `docker.Client.CreateSecret(ctx, spec swarm.SecretSpec) (string, error)` (`internal/docker/client.go:601`), `CreateConfig` (`:593`).
- Produces: `createResult` struct with `ID string`, `Name string`, `Type string`.

Swarm secrets are immutable, so rotation is create-new → repoint service → drop old. Cetacean could only ever do the third step. This is the first.

Tier 2 (configuration): creating a secret grants nothing on its own — nothing references it until Task 3's tool points a service at it.

- [ ] **Step 1: Write the failing test**

```go
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCreateSecretReturnsItsID(t *testing.T) {
	srv, writes := newCreateTestServer(t)

	body, err := srv.toolCreateSecret(context.Background(), toolRequest(t, "create_secret", map[string]any{
		"name": "db_password_v2",
		"data": "hunter2",
	}))
	if err != nil {
		t.Fatalf("toolCreateSecret: %v", err)
	}

	var got createResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Name != "db_password_v2" {
		t.Errorf("name = %q, want db_password_v2", got.Name)
	}
	if got.ID == "" {
		t.Error("ID is empty; the caller needs it to point a service at the secret")
	}
	if writes.secretSpec.Name != "db_password_v2" {
		t.Errorf("spec name = %q", writes.secretSpec.Name)
	}
}

// The payload must reach Docker as bytes, not as the base64 the caller sent —
// getting this backwards writes a secret whose value is the encoding of the
// value, and nothing detects it until something fails to authenticate.
func TestCreateSecretDecodesBase64Data(t *testing.T) {
	srv, writes := newCreateTestServer(t)

	// "hunter2" base64-encoded.
	_, err := srv.toolCreateSecret(context.Background(), toolRequest(t, "create_secret", map[string]any{
		"name":     "db_password_v2",
		"data":     "aHVudGVyMg==",
		"encoding": "base64",
	}))
	if err != nil {
		t.Fatalf("toolCreateSecret: %v", err)
	}

	if string(writes.secretSpec.Data) != "hunter2" {
		t.Errorf("data = %q, want the decoded payload", writes.secretSpec.Data)
	}
}

// A secret's value must never come back out.
func TestCreateSecretNeverEchoesTheData(t *testing.T) {
	srv, _ := newCreateTestServer(t)

	body, err := srv.toolCreateSecret(context.Background(), toolRequest(t, "create_secret", map[string]any{
		"name": "db_password_v2",
		"data": "hunter2",
	}))
	if err != nil {
		t.Fatalf("toolCreateSecret: %v", err)
	}

	if strings.Contains(body, "hunter2") {
		t.Errorf("the reply echoes the secret payload: %s", body)
	}
}

func TestCreateConfigReturnsItsID(t *testing.T) {
	srv, writes := newCreateTestServer(t)

	body, err := srv.toolCreateConfig(context.Background(), toolRequest(t, "create_config", map[string]any{
		"name": "nginx_conf",
		"data": "server {}",
	}))
	if err != nil {
		t.Fatalf("toolCreateConfig: %v", err)
	}

	var got createResult
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Type != "config" {
		t.Errorf("type = %q, want config", got.Type)
	}
	if string(writes.configSpec.Data) != "server {}" {
		t.Errorf("data = %q", writes.configSpec.Data)
	}
}
```

Write `newCreateTestServer` in the same file, modelled on `newUpdateTestServer`, with a fake capturing `secretSpec swarm.SecretSpec` and `configSpec swarm.ConfigSpec`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/mcp/ -run 'TestCreateSecret|TestCreateConfig'`
Expected: FAIL — `undefined: toolCreateSecret`.

- [ ] **Step 3: Write the tools**

```go
package mcp

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// createResult is what a create answers with: enough to reference the new
// resource and nothing more. A secret's payload is never echoed — the caller
// supplied it, and repeating it puts it in a transcript.
type createResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// decodePayload reads the `data` argument, honouring `encoding`.
//
// Base64 exists because a config is often a file — a TLS certificate, an nginx
// block — and JSON string escaping mangles one. Getting the direction wrong
// writes a secret whose value is the encoding of the value, which nothing
// detects until an authentication fails at runtime, so the encoding is named
// explicitly rather than guessed from the shape of the string.
func decodePayload(req mcplib.CallToolRequest) ([]byte, error) {
	raw := req.GetString("data", "")
	if raw == "" {
		return nil, fmt.Errorf("data: required")
	}

	switch encoding := req.GetString("encoding", "utf8"); encoding {
	case "utf8":
		return []byte(raw), nil

	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("data: not valid base64 (encoding was given as base64): %w", err)
		}

		return decoded, nil

	default:
		return nil, fmt.Errorf("encoding %q: want \"utf8\" or \"base64\"", encoding)
	}
}

// toolCreateSecret creates a Swarm secret.
//
// Swarm secrets are immutable, so "update the secret of foo" is create-new,
// repoint the service, drop the old — and Cetacean could previously only do
// the third step, which made the whole sequence impossible. This is the first.
func (s *Server) toolCreateSecret(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	name := req.GetString("name", "")
	if name == "" {
		return "", fmt.Errorf("name: required")
	}

	if err := s.checkWrite(ctx, "secret", name); err != nil {
		return "", err
	}

	data, err := decodePayload(req)
	if err != nil {
		return "", err
	}

	labels, err := optionalStringMap(req, "labels")
	if err != nil {
		return "", err
	}

	id, err := s.writes.CreateSecret(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        data,
	})
	if err != nil {
		return "", fmt.Errorf("create secret: %w", err)
	}

	return marshalResult(createResult{ID: id, Name: name, Type: "secret"})
}

// toolCreateConfig creates a Swarm config. Unlike a secret, a config's content
// is readable afterwards; it is still not echoed here, because the caller
// already has it and describe will serve it.
func (s *Server) toolCreateConfig(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	name := req.GetString("name", "")
	if name == "" {
		return "", fmt.Errorf("name: required")
	}

	if err := s.checkWrite(ctx, "config", name); err != nil {
		return "", err
	}

	data, err := decodePayload(req)
	if err != nil {
		return "", err
	}

	labels, err := optionalStringMap(req, "labels")
	if err != nil {
		return "", err
	}

	id, err := s.writes.CreateConfig(ctx, swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        data,
	})
	if err != nil {
		return "", fmt.Errorf("create config: %w", err)
	}

	return marshalResult(createResult{ID: id, Name: name, Type: "config"})
}
```

**Note:** `checkWrite` and `optionalStringMap` — check whether these exist under those names in `internal/mcp`. `checkRead` exists; find its write counterpart (the mutating tools all perform one). If `optionalStringMap` does not exist, decode `labels` with the existing `decodeArgInto` helper into a `map[string]string`.

- [ ] **Step 4: Register both tools**

Add a `ResourceCreator` interface to `internal/mcp/tools.go` and fold it into the composite `DockerWriteClient`:

```go
// ResourceCreator creates the two resource types a service can reference by
// name. Narrow, like the other write interfaces here, so a test fake
// implements only what it exercises.
type ResourceCreator interface {
	CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error)
	CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error)
}
```

Add to `toolCatalog()` in the tier-2 block:

```go
{
	tool: mcplib.NewTool(
		"create_secret",
		mcplib.WithToolTitle("Create a secret"),
		mcplib.WithDescription(
			"Create a Swarm secret. Swarm secrets cannot be modified, so rotating one means creating a replacement, pointing the services at it with update_service_secrets, and removing the old one — this is the first step. The value is write-only: it is never returned by this or any other tool. Pass `encoding: \"base64\"` for binary content or anything JSON string escaping would mangle, such as a certificate or a key file.",
		),
		mcplib.WithOutputSchema[createResult](),
		mcplib.WithReadOnlyHintAnnotation(false),
		mcplib.WithDestructiveHintAnnotation(false),
		mcplib.WithString("name", mcplib.Required(), mcplib.Description("Name for the new secret. Must be unique in the cluster.")),
		mcplib.WithString("data", mcplib.Required(), mcplib.Description("The secret's value.")),
		mcplib.WithString("encoding", mcplib.Description("How `data` is encoded: \"utf8\" (default) or \"base64\".")),
		mcplib.WithObject("labels", mcplib.Description("Labels to set on the secret.")),
	),
	tier:    config.OpsConfiguration,
	handler: s.toolCreateSecret,
},
```

and the parallel `create_config` entry, with `"Create a Swarm config. Unlike a secret, its content can be read back afterwards. Pass encoding: \"base64\" for a file whose content JSON string escaping would mangle."`.

Add to `toolACLSpecs` in `internal/mcp/server.go`:

```go
	"create_secret": {resourceType: "secret", permission: "write"},
	"create_config": {resourceType: "config", permission: "write"},
```

Add `"create_secret": "edit"` and `"create_config": "edit"` to `toolIconCategory`.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/mcp/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/creates.go internal/mcp/creates_test.go internal/mcp/tools.go internal/mcp/server.go
git commit -m "feat(mcp): add create_secret and create_config"
```

---

### Task 3: `update_service_secrets` and `update_service_configs`

**Files:**
- Create: `internal/mcp/attachments.go`
- Modify: `internal/mcp/tools.go`, `internal/mcp/server.go`, `internal/cluster/view.go`
- Test: `internal/mcp/attachments_test.go`

**Interfaces:**
- Consumes: `docker.Client.UpdateServiceSecrets` (`internal/docker/client.go:1014`), `UpdateServiceConfigs` (`:988`) — read both signatures before writing the interface.
- Produces: `attachmentResult`, reusing `serviceUpdateResult`'s shape where possible.

Tier 2 (configuration), matching the REST route for the same operation — see "Two decisions this plan makes explicit" above.

Together with Task 2 this closes intent 53, "update the secret of foo".

- [ ] **Step 1: Extend `ServiceDetails` with the attachment names**

A write answers with the section it changed, so the projection must carry it. Test first, in `internal/cluster/view_test.go`:

```go
// Names, never content: a service digest may say which secrets a container
// receives without becoming a way to read them.
func TestServiceDetailsReportsAttachmentNames(t *testing.T) {
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{
						{SecretID: "s1", SecretName: "db_password"},
					},
					Configs: []*swarm.ConfigReference{
						{ConfigID: "c1", ConfigName: "nginx_conf"},
					},
				},
			},
		},
	}

	got := ServiceDetails(svc)

	names, ok := got["secretNames"].([]string)
	if !ok || len(names) != 1 || names[0] != "db_password" {
		t.Errorf("secretNames = %v, want [db_password]", got["secretNames"])
	}
	if cfgs, ok := got["configNames"].([]string); !ok || cfgs[0] != "nginx_conf" {
		t.Errorf("configNames = %v, want [nginx_conf]", got["configNames"])
	}
}
```

Run it, watch it fail, then add to `ServiceDetails` (sorted, per the determinism rule):

```go
	if refs := containerSpec(svc).Secrets; len(refs) > 0 {
		names := make([]string, 0, len(refs))
		for _, ref := range refs {
			names = append(names, ref.SecretName)
		}
		sort.Strings(names)
		details["secretNames"] = names
	}

	if refs := containerSpec(svc).Configs; len(refs) > 0 {
		names := make([]string, 0, len(refs))
		for _, ref := range refs {
			names = append(names, ref.ConfigName)
		}
		sort.Strings(names)
		details["configNames"] = names
	}
```

- [ ] **Step 2: Write the failing MCP test**

```go
package mcp

import (
	"context"
	"strings"
	"testing"
)

// The second step of a secret rotation, and the reason create_secret exists.
func TestUpdateServiceSecretsRepointsAService(t *testing.T) {
	c, srv, writes := newUpdateTestServer(t)
	seedService(t, c, "svc1", "web")
	seedSecret(t, c, "s2", "db_password_v2")

	_, err := srv.toolUpdateServiceSecrets(context.Background(), toolRequest(t, "update_service_secrets", map[string]any{
		"id": "web",
		"secrets": []any{
			map[string]any{"name": "db_password_v2", "target": "/run/secrets/db_password"},
		},
	}))
	if err != nil {
		t.Fatalf("toolUpdateServiceSecrets: %v", err)
	}

	if len(writes.secretRefs) != 1 {
		t.Fatalf("secret refs = %d, want 1", len(writes.secretRefs))
	}
	if writes.secretRefs[0].SecretName != "db_password_v2" {
		t.Errorf("name = %q", writes.secretRefs[0].SecretName)
	}
	// The ID must be resolved from the name: Docker rejects a reference whose
	// ID and name disagree, and a model has the name, not the ID.
	if writes.secretRefs[0].SecretID != "s2" {
		t.Errorf("SecretID = %q, want the resolved ID s2", writes.secretRefs[0].SecretID)
	}
}

// A secret that does not exist must be refused before the write, naming it —
// Docker's own error for this is opaque.
func TestUpdateServiceSecretsRejectsAnUnknownSecret(t *testing.T) {
	c, srv, _ := newUpdateTestServer(t)
	seedService(t, c, "svc1", "web")

	_, err := srv.toolUpdateServiceSecrets(context.Background(), toolRequest(t, "update_service_secrets", map[string]any{
		"id":      "web",
		"secrets": []any{map[string]any{"name": "nosuch"}},
	}))
	if err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("expected an error naming the missing secret, got %v", err)
	}
}

// Replacement, not merge — the caller passes the complete list, and an empty
// list detaches every secret. Saying so in the description is not enough;
// the behaviour is pinned here.
func TestUpdateServiceSecretsReplacesWholesale(t *testing.T) {
	c, srv, writes := newUpdateTestServer(t)
	seedService(t, c, "svc1", "web")

	_, err := srv.toolUpdateServiceSecrets(context.Background(), toolRequest(t, "update_service_secrets", map[string]any{
		"id":      "web",
		"secrets": []any{},
	}))
	if err != nil {
		t.Fatalf("toolUpdateServiceSecrets: %v", err)
	}

	if len(writes.secretRefs) != 0 {
		t.Errorf("refs = %v, want none", writes.secretRefs)
	}
	if !writes.secretsCalled {
		t.Error("the write was skipped entirely; an empty list must still detach")
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/mcp/ -run TestUpdateServiceSecrets`
Expected: FAIL — `undefined: toolUpdateServiceSecrets`.

- [ ] **Step 4: Write the tools**

```go
package mcp

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// defaultSecretMountRoot is where Swarm places a secret when the caller does
// not say. Docker's own default, repeated here because the wire shape lets
// target be omitted.
const defaultSecretMountRoot = "/run/secrets/"

// attachmentRef is one secret or config a container should receive.
type attachmentRef struct {
	// Name is the secret's or config's name. The ID is resolved from it,
	// because a model holds names — every listing offers names, completion
	// offers names — while Docker's reference type wants both and rejects a
	// pair that disagrees.
	Name string `json:"name"`

	// Target is the absolute path inside the container. Defaults to
	// /run/secrets/<name> for a secret, /<name> for a config.
	Target string `json:"target,omitempty"`

	Mode uint32 `json:"mode,omitempty"`
}

// toolUpdateServiceSecrets replaces the set of secrets a service receives.
//
// This is the second step of a rotation — create the replacement, repoint the
// service, drop the old — and it sits at the configuration level, the same one
// the REST route for this operation requires. The spec argued for raising it,
// on the grounds that handing a container a different credential is a bigger
// step for an agent; that was rejected in favour of the operations level
// meaning one thing whichever transport an operator reaches for.
//
// The list replaces rather than merges, like every other wholesale section:
// passing an empty list detaches every secret.
func (s *Server) toolUpdateServiceSecrets(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	svc, err := s.resolveServiceForWrite(ctx, req)
	if err != nil {
		return "", err
	}

	var refs []attachmentRef
	if err := decodeArgInto(req, "secrets", &refs); err != nil {
		return "", fmt.Errorf("secrets: %w", err)
	}

	resolved := make([]*swarm.SecretReference, 0, len(refs))

	for _, ref := range refs {
		sec, found, err := s.cache.ResolveSecret(ref.Name)
		if err != nil {
			return "", err
		}
		if !found {
			return "", fmt.Errorf(
				"no such secret %q; create it with create_secret, or list what exists with find type \"secrets\"",
				ref.Name,
			)
		}

		// The caller must be allowed to read the secret they are attaching,
		// or this becomes a way to mount a credential they cannot see.
		if err := s.checkRead(ctx, "secret", sec.Spec.Name); err != nil {
			return "", err
		}

		target := ref.Target
		if target == "" {
			target = defaultSecretMountRoot + sec.Spec.Name
		}

		mode := ref.Mode
		if mode == 0 {
			mode = 0o444
		}

		resolved = append(resolved, &swarm.SecretReference{
			SecretID:   sec.ID,
			SecretName: sec.Spec.Name,
			File: &swarm.SecretReferenceFileTarget{
				Name: target,
				UID:  "0",
				GID:  "0",
				Mode: os.FileMode(mode),
			},
		})
	}

	updated, err := s.writes.UpdateServiceSecrets(ctx, svc.ID, resolved)
	if err != nil {
		return "", fmt.Errorf("update service secrets: %w", err)
	}

	return s.serviceUpdate(updated, sectionSecrets)
}
```

Write `toolUpdateServiceConfigs` as the exact parallel, using `swarm.ConfigReference`, `ResolveConfig`, ACL type `"config"`, and a default target of `"/" + name`.

Add section constants `sectionSecrets = "secrets"` and `sectionConfigs = "configs"` with `serviceSectionKeys` entries `{"secretNames"}` and `{"configNames"}`, so `serviceUpdate` can project the reply.

**Note:** `resolveServiceForWrite` is shorthand for what the existing mutating tools do — resolve the id argument, then `checkWrite(ctx, "service", name)`. Find the existing helper rather than writing a new one; `toolUpdateService` already does exactly this.

- [ ] **Step 5: Register both tools**

Tier 2 (`config.OpsConfiguration`), in `toolCatalog()`. Both carry `mcplib.WithDestructiveHintAnnotation(true)` — replacing the set detaches whatever is not listed, and a running container loses a credential it may need.

```go
mcplib.WithDescription(
	"Replace the complete set of secrets a service's containers receive. Swarm secrets cannot be modified in place, so rotating one is: create_secret for the replacement, this tool to repoint the service, then remove_secret for the old one. The list replaces rather than merges — pass every secret the service should have, because one you omit is detached — and each entry names a secret rather than giving its ID. Triggers a rolling deploy. You must be permitted to read a secret to attach it.",
),
```

Add to `toolACLSpecs`: both `{resourceType: "service", permission: "write"}`. Add both to `toolIconCategory` as `"edit"`.

- [ ] **Step 6: Run to verify it passes**

Run: `go test ./internal/mcp/ ./internal/cluster/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/attachments.go internal/mcp/attachments_test.go internal/cluster/view.go internal/cluster/view_test.go internal/mcp/tools.go internal/mcp/server.go
git commit -m "feat(mcp): add update_service_secrets and update_service_configs"
```

---

### Task 4: `update_service_mounts`

**Files:**
- Modify: `internal/mcp/attachments.go`, `internal/mcp/tools.go`, `internal/mcp/server.go`, `internal/cluster/view.go`
- Test: `internal/mcp/attachments_test.go`

**Interfaces:**
- Consumes: `docker.Client.UpdateServiceMounts` (`internal/docker/client.go:1063`) — read the signature first.

Tier 2, matching REST. Binding `/var/run/docker.sock` into a container turns a configuration change into a root shell on the host, and the tool's description must say so plainly — that warning is where the danger is communicated, since the tier no longer carries it.

- [ ] **Step 1: Write the failing test**

```go
// A bind mount of the Docker socket is a host root shell. The tool does not
// refuse it — an operator at this level may legitimately want it, and Cetacean
// does not decide for the caller — but since the tier deliberately matches
// REST rather than being raised, the description is the only place the danger
// is communicated, so it has to be there.
func TestUpdateServiceMountsMatchesTheRESTTier(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	for _, def := range srv.toolCatalog() {
		if def.tool.Name == "update_service_mounts" {
			if def.tier != config.OpsConfiguration {
				t.Errorf("tier = %v, want OpsConfiguration to match the REST route for the same operation", def.tier)
			}
			if !strings.Contains(strings.ToLower(def.tool.Description), "host") {
				t.Error("the description does not warn about host binds")
			}

			return
		}
	}

	t.Fatal("update_service_mounts is not registered")
}

func TestUpdateServiceMountsReplacesTheSet(t *testing.T) {
	c, srv, writes := newUpdateTestServer(t)
	seedService(t, c, "svc1", "web")

	_, err := srv.toolUpdateServiceMounts(context.Background(), toolRequest(t, "update_service_mounts", map[string]any{
		"id": "web",
		"mounts": []any{
			map[string]any{"type": "volume", "source": "data", "target": "/var/lib/data"},
		},
	}))
	if err != nil {
		t.Fatalf("toolUpdateServiceMounts: %v", err)
	}

	if len(writes.mounts) != 1 {
		t.Fatalf("mounts = %d, want 1", len(writes.mounts))
	}
	if writes.mounts[0].Target != "/var/lib/data" {
		t.Errorf("target = %q", writes.mounts[0].Target)
	}
}

// An unknown mount type must be named rather than passed to Docker, which
// answers with a less useful error.
func TestUpdateServiceMountsRejectsAnUnknownType(t *testing.T) {
	c, srv, _ := newUpdateTestServer(t)
	seedService(t, c, "svc1", "web")

	_, err := srv.toolUpdateServiceMounts(context.Background(), toolRequest(t, "update_service_mounts", map[string]any{
		"id":     "web",
		"mounts": []any{map[string]any{"type": "magic", "target": "/x"}},
	}))
	if err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("expected an error naming the bad type, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/mcp/ -run TestUpdateServiceMounts`
Expected: FAIL — `undefined: toolUpdateServiceMounts`.

- [ ] **Step 3: Write the tool**

Follow the shape of Task 3's tools. Decode into a `mountValue` struct (`Type`, `Source`, `Target`, `ReadOnly`), validate `Type` against `"volume"`, `"bind"` and `"tmpfs"` by name, convert to `[]mount.Mount`, and call `s.writes.UpdateServiceMounts`. Add `sectionMounts = "mounts"` with `serviceSectionKeys` entry `{"mountTargets"}`, and extend `ServiceDetails` with a sorted `mountTargets []string` the same way Task 3 added `secretNames` (test it first).

- [ ] **Step 4: Register the tool**

Tier 2 (`config.OpsConfiguration`), `WithDestructiveHintAnnotation(true)`, with this description:

```
"Replace the complete set of filesystem mounts a service's containers receive: named volumes, host bind mounts and tmpfs. The list replaces rather than merges — a mount you omit is removed, and a container may lose data it was writing to. Triggers a rolling deploy. Note that a bind mount gives the container access to the host's filesystem at that path, and binding the Docker socket gives it control of the whole cluster; this is why the tool sits at the impactful operations level."
```

Add `toolACLSpecs` and `toolIconCategory` entries as in Task 3.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./internal/mcp/ ./internal/cluster/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/attachments.go internal/mcp/attachments_test.go internal/cluster/view.go internal/cluster/view_test.go internal/mcp/tools.go internal/mcp/server.go
git commit -m "feat(mcp): add update_service_mounts"
```

---

### Task 5: Documentation, changelog and live verification

**Files:**
- Modify: `docs/mcp.md`, `CHANGELOG.md`

- [ ] **Step 1: Document the new tools in `docs/mcp.md`**

Add them to the tier-2 table, and document `create_secret`/`create_config` with the rotation sequence spelled out, since it is three tools that must run in order and an agent needs to know the order:

```markdown
Swarm secrets are immutable, so rotating one is three calls in order: `create_secret` for the replacement,
`update_service_secrets` to repoint each service that uses it — `describe` the secret first, its `related` array
names them — and `remove_secret` for the old one once nothing references it. Cetacean could previously only do
the last of the three.
```

Every MCP tool requires the same operations level as the REST route for the same operation; there is no separate
MCP tier table to keep in step.

- [ ] **Step 2: Add changelog entries**

Under `### Added`:

```markdown
- AI agents can now add a health check to a service, or change its command and arguments. Cetacean already reported which services had no health check and had no way to fix it
- AI agents can now create secrets and configs, and change which ones a service receives. Swarm secrets cannot be edited, so rotating a password means creating a replacement, pointing the services at it and removing the old one — Cetacean could previously only do the last step, which made the whole sequence impossible
- AI agents can now change a service's volume, bind and tmpfs mounts
```

No `### Changed` entry is needed: the three attachment editors sit at the same operations level as the REST routes for the same operations, so nothing an operator already knows has changed.

- [ ] **Step 3: Verify the whole suite and lint**

```bash
go test ./...
make lint
make fmt-check
```

- [ ] **Step 4: Verify against a live cluster**

Follow `.claude/skills/run-cetacean/SKILL.md` and the MCP-over-HTTP recipe in the read-verbs plan's Task 6 (the `2026-07-28` header contract is enforced strictly). Run at `CETACEAN_OPERATIONS_LEVEL=3` on a spare port, against the demo stack, and confirm:

- `create_secret` returns an ID, and `find type: "secrets"` then lists it.
- `update_service_secrets` repoints `demo_web` at it and the service redeploys; `describe` shows the new `secretNames`.
- `update_service` with `section: "healthcheck"` adds a check to `demo_web`, and `get_recommendations` stops reporting `no-healthcheck` for it — that round trip is the whole point of this plan.
- At `CETACEAN_OPERATIONS_LEVEL=2`, `tools/list` contains all five new tools; at level 1 it contains none of them. This is the check that the tier matches REST rather than having drifted a level up.

Kill the process and delete the binary afterwards.

- [ ] **Step 5: Commit**

```bash
git add docs/mcp.md CHANGELOG.md
git commit -m "docs(mcp): document the write surface additions and the tier divergence"
```

---

## Self-Review

**Spec coverage.** The spec's write table has six rows of change. `update_service_config` merging the eight tier-2 editors shipped in `b46730c8` as `update_service`. This plan covers the rest: `healthcheck` and `command` (Task 1), `create_secret`/`create_config` (Task 2), `update_service_secrets`/`update_service_configs` (Task 3), `update_service_mounts` (Task 4). `update_node_labels` and the five `remove_*` tools are unchanged, as the spec says. Intents closed: 53, 56, 57, 58, and with them 60 and 63, which the spec says fall out once these primitives exist.

**Placeholder scan.** Four steps say "read the existing signature first" rather than quoting it — Task 1 Step 7 (`UpdateServiceContainerConfig`), Task 3 Step 4 (`UpdateServiceSecrets`/`UpdateServiceConfigs`), Task 4 Step 3 (`UpdateServiceMounts`), and the `checkWrite`/`optionalStringMap`/`resolveServiceForWrite` helper notes. These are deliberate: the methods exist and are already exercised by `internal/api`, and quoting a signature I have not read would be worse than pointing at the file and line. Every one names the exact file and line to read.

**Type consistency.** `attachmentRef` (Task 3) is reused by `toolUpdateServiceConfigs`. `createResult` (Task 2) is used by both create tools. Section constants follow the existing kebab-case convention only where the shipped ones do (`update-policy`); the new ones are single words, so `healthcheck`, `command`, `secrets`, `configs`, `mounts`. `serviceUpdate(updated, section)` is the existing reply builder from `internal/mcp/updates.go` and every new tool ends by calling it, which is what keeps the no-leak guarantee from `65553bfd`.

**Ordering.** Task 3 depends on Task 2 only conceptually (rotation needs both) — they can be implemented in either order. Task 4 is independent of both. Task 1 is independent of all of them and is the highest-value single task, since it closes the diagnose-without-treat gap the evaluation found most glaring; start there.
