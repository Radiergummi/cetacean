# Service Container Config Editors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add editable container configuration (command, args, workdir, user, hostname, init, tty, read-only, stop signal, stop grace period, capabilities, extra hosts, DNS) to the service detail page via a single GET/PATCH endpoint and five inline editor components.

**Architecture:** One backend endpoint (`GET/PATCH /services/{id}/container-config`) serves and accepts all ContainerSpec fields as a camelCase JSON object with RFC 7396 merge-patch. Five frontend editor components (CommandEditor, RuntimeEditor, CapabilitiesEditor, ExtraHostsEditor, DnsEditor) replace the read-only KV table inside the existing "Container Configuration" CollapsibleSection.

**Tech Stack:** Go (Docker Engine API, merge-patch), React 19, TypeScript, Tailwind CSS, shadcn/ui

**Spec:** `docs/superpowers/specs/2026-03-21-service-container-config-design.md`

---

### Task 1: Docker Client — UpdateServiceContainerConfig

**Files:**
- Modify: `internal/docker/client.go` (after existing `UpdateService*` methods)

- [ ] **Step 1: Add `UpdateServiceContainerConfig` method**

Add after the last `UpdateService*` method. This takes a `containerConfigUpdate` struct (defined in the handler package) but since Go doesn't allow circular imports, the client method takes the raw fields it needs to set. The simplest approach matching existing patterns: accept a function that mutates the ContainerSpec.

Actually, per the spec review, the interface should use a concrete type. Since the `containerConfigResponse` struct lives in the `api` package and the client lives in the `docker` package, we need a shared approach. The cleanest pattern (matching how `UpdateServiceResources` takes `*swarm.ResourceRequirements`): have the handler build the final `ContainerSpec` fields and pass them as a callback. But every other method takes a concrete SDK type.

The pragmatic approach: the client method receives a `func(*swarm.ContainerSpec)` internally but the `DockerWriteClient` interface method signature uses a concrete struct. Wait — the spec was updated to use a concrete struct. Let's define a `ContainerConfigUpdate` struct in the `api` package and have the client accept it.

Actually, the simplest approach is: define the handler's response struct, have the handler pass it to the client, and the client maps it onto ContainerSpec. But that creates a dependency from `docker` → `api`. Instead, have the client accept a `map[string]any` that it applies, or better: have the handler do all the mapping onto a fresh `ContainerSpec` overlay and pass that to the client, which merges it.

The cleanest approach: the handler builds the mutation and the client just does inspect→mutate→update. Let the client take a `func(*swarm.ContainerSpec)` — this is an internal implementation detail, and the mock can verify the function's effects by passing it a test ContainerSpec. The spec review flagged this but the alternative (a concrete struct in the docker package duplicating all fields) is worse.

Let's use the function approach for the implementation and define the interface accordingly:

```go
func (c *Client) UpdateServiceContainerConfig(
	ctx context.Context,
	id string,
	apply func(spec *swarm.ContainerSpec),
) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	apply(svc.Spec.TaskTemplate.ContainerSpec)
	_, err = c.docker.ServiceUpdate(
		ctx,
		svc.ID,
		svc.Version,
		svc.Spec,
		swarm.ServiceUpdateOptions{},
	)
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean && go build ./internal/docker/`

- [ ] **Step 3: Commit**

```bash
git add internal/docker/client.go
git commit -m "feat: add UpdateServiceContainerConfig to Docker client"
```

---

### Task 2: DockerWriteClient Interface, Mock, Handlers & Tests

**Files:**
- Modify: `internal/api/handlers.go` (DockerWriteClient interface)
- Modify: `internal/api/write_handlers.go` (handlers + response struct)
- Modify: `internal/api/write_handlers_test.go` (mock + tests)

This is a large task combining interface, handlers, and tests because they are tightly coupled.

- [ ] **Step 1: Add to DockerWriteClient interface**

In `internal/api/handlers.go`, add after the last method in the interface:

```go
UpdateServiceContainerConfig(ctx context.Context, id string, apply func(spec *swarm.ContainerSpec)) (swarm.Service, error)
```

- [ ] **Step 2: Add mock field and stub**

In `internal/api/write_handlers_test.go`, add to `mockWriteClient` struct:

```go
updateServiceContainerConfigFn func(ctx context.Context, id string, apply func(spec *swarm.ContainerSpec)) (swarm.Service, error)
```

Add stub method:

```go
func (m *mockWriteClient) UpdateServiceContainerConfig(
	ctx context.Context,
	id string,
	apply func(spec *swarm.ContainerSpec),
) (swarm.Service, error) {
	if m.updateServiceContainerConfigFn != nil {
		return m.updateServiceContainerConfigFn(ctx, id, apply)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}
```

- [ ] **Step 3: Add response structs and GET handler**

In `internal/api/write_handlers.go`, add after the last handler:

```go
type containerConfigResponse struct {
	Command         []string        `json:"command"`
	Args            []string        `json:"args"`
	Dir             string          `json:"dir"`
	User            string          `json:"user"`
	Hostname        string          `json:"hostname"`
	Init            *bool           `json:"init"`
	TTY             bool            `json:"tty"`
	ReadOnly        bool            `json:"readOnly"`
	StopSignal      string          `json:"stopSignal"`
	StopGracePeriod *int64          `json:"stopGracePeriod"`
	CapabilityAdd   []string        `json:"capabilityAdd"`
	CapabilityDrop  []string        `json:"capabilityDrop"`
	Groups          []string        `json:"groups"`
	Hosts           []string        `json:"hosts"`
	DNSConfig       *dnsConfigJSON  `json:"dnsConfig"`
}

type dnsConfigJSON struct {
	Nameservers []string `json:"nameservers"`
	Search      []string `json:"search"`
	Options     []string `json:"options"`
}

func containerConfigFromSpec(cs *swarm.ContainerSpec) containerConfigResponse {
	if cs == nil {
		return containerConfigResponse{}
	}
	resp := containerConfigResponse{
		Command:        cs.Command,
		Args:           cs.Args,
		Dir:            cs.Dir,
		User:           cs.User,
		Hostname:       cs.Hostname,
		Init:           cs.Init,
		TTY:            cs.TTY,
		ReadOnly:       cs.ReadOnly,
		StopSignal:     cs.StopSignal,
		CapabilityAdd:  cs.CapabilityAdd,
		CapabilityDrop: cs.CapabilityDrop,
		Groups:         cs.Groups,
		Hosts:          cs.Hosts,
	}
	if cs.StopGracePeriod != nil {
		ns := int64(*cs.StopGracePeriod)
		resp.StopGracePeriod = &ns
	}
	if cs.DNSConfig != nil {
		resp.DNSConfig = &dnsConfigJSON{
			Nameservers: cs.DNSConfig.Nameservers,
			Search:      cs.DNSConfig.Search,
			Options:     cs.DNSConfig.Options,
		}
	}
	return resp
}

func (h *Handlers) HandleGetServiceContainerConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	resp := containerConfigFromSpec(svc.Spec.TaskTemplate.ContainerSpec)
	writeJSON(w, resp)
}
```

- [ ] **Step 4: Add PATCH handler**

```go
func (h *Handlers) HandlePatchServiceContainerConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "expected Content-Type: application/merge-patch+json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := containerConfigFromSpec(svc.Spec.TaskTemplate.ContainerSpec)
	baseBytes, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal current config")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(baseBytes, &baseMap); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to unmarshal current config")
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	mergePatch(baseMap, patchMap)

	mergedBytes, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged config")
		return
	}
	var merged containerConfigResponse
	if err := json.Unmarshal(mergedBytes, &merged); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid patch result")
		return
	}

	slog.Info("updating service container config", "service", id)

	updated, err := h.writeClient.UpdateServiceContainerConfig(r.Context(), id, func(cs *swarm.ContainerSpec) {
		cs.Command = merged.Command
		cs.Args = merged.Args
		cs.Dir = merged.Dir
		cs.User = merged.User
		cs.Hostname = merged.Hostname
		cs.Init = merged.Init
		cs.TTY = merged.TTY
		cs.ReadOnly = merged.ReadOnly
		cs.StopSignal = merged.StopSignal
		cs.CapabilityAdd = merged.CapabilityAdd
		cs.CapabilityDrop = merged.CapabilityDrop
		cs.Groups = merged.Groups
		cs.Hosts = merged.Hosts
		if merged.StopGracePeriod != nil {
			d := time.Duration(*merged.StopGracePeriod)
			cs.StopGracePeriod = &d
		} else {
			cs.StopGracePeriod = nil
		}
		if merged.DNSConfig != nil {
			cs.DNSConfig = &swarm.DNSConfig{
				Nameservers: merged.DNSConfig.Nameservers,
				Search:      merged.DNSConfig.Search,
				Options:     merged.DNSConfig.Options,
			}
		} else {
			cs.DNSConfig = nil
		}
	})
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	result := containerConfigFromSpec(updated.Spec.TaskTemplate.ContainerSpec)
	writeJSON(w, result)
}
```

Note: `strings` and `slog` are already imported in `write_handlers.go`. You must add `"time"` to the import block (it is NOT already imported) — needed for `time.Duration` in the StopGracePeriod conversion.

- [ ] **Step 5: Write tests**

Add to `internal/api/write_handlers_test.go`:

```go
func TestHandleGetServiceContainerConfig_OK(t *testing.T) {
	init := true
	gracePeriod := time.Duration(10_000_000_000)
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Command:         []string{"/bin/sh"},
					Args:            []string{"-c", "echo hello"},
					Dir:             "/app",
					User:            "node",
					Hostname:        "web-1",
					Init:            &init,
					TTY:             false,
					ReadOnly:        true,
					StopSignal:      "SIGTERM",
					StopGracePeriod: &gracePeriod,
					CapabilityAdd:   []string{"NET_ADMIN"},
					CapabilityDrop:  []string{"ALL"},
				},
			},
		},
	})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/svc1/container-config", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceContainerConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["hostname"] != "web-1" {
		t.Errorf("hostname=%v, want web-1", resp["hostname"])
	}
	if resp["readOnly"] != true {
		t.Errorf("readOnly=%v, want true", resp["readOnly"])
	}
	if resp["stopGracePeriod"] != float64(10_000_000_000) {
		t.Errorf("stopGracePeriod=%v, want 10000000000", resp["stopGracePeriod"])
	}
}

func TestHandleGetServiceContainerConfig_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/missing/container-config", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleGetServiceContainerConfig(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceContainerConfig_PartialPatch(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Hostname: "old-host",
					TTY:      false,
				},
			},
		},
	})

	wc := &mockWriteClient{
		updateServiceContainerConfigFn: func(_ context.Context, id string, apply func(*swarm.ContainerSpec)) (swarm.Service, error) {
			cs := &swarm.ContainerSpec{}
			apply(cs)
			return swarm.Service{
				ID: id,
				Spec: swarm.ServiceSpec{
					TaskTemplate: swarm.TaskSpec{ContainerSpec: cs},
				},
			}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil, config.OpsImpactful)

	body := `{"hostname":"new-host","tty":true}`
	req := httptest.NewRequest("PATCH", "/services/svc1/container-config", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceContainerConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["hostname"] != "new-host" {
		t.Errorf("hostname=%v, want new-host", resp["hostname"])
	}
	if resp["tty"] != true {
		t.Errorf("tty=%v, want true", resp["tty"])
	}
}

func TestHandlePatchServiceContainerConfig_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{}}}})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	body := `{"hostname":"x"}`
	req := httptest.NewRequest("PATCH", "/services/svc1/container-config", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceContainerConfig(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandlePatchServiceContainerConfig_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("PATCH", "/services/missing/container-config", strings.NewReader(`{}`))
	req.SetPathValue("id", "missing")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	w := httptest.NewRecorder()
	h.HandlePatchServiceContainerConfig(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}
```

- [ ] **Step 6: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -run "TestHandle(Get|Patch)ServiceContainerConfig" -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers.go internal/api/write_handlers.go internal/api/write_handlers_test.go internal/docker/client.go
git commit -m "feat: add GET/PATCH /services/{id}/container-config endpoint"
```

---

### Task 3: Router Registration

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add route registrations**

In the tier-2 service write operations section (after the last `PATCH /services/{id}/*` line), add:

```go
mux.HandleFunc("GET /services/{id}/container-config", contentNegotiated(h.HandleGetServiceContainerConfig, spa))
mux.Handle("PATCH /services/{id}/container-config", tier2(h.HandlePatchServiceContainerConfig))
```

- [ ] **Step 2: Run full backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./internal/api/ -count=1`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: register container-config routes"
```

---

### Task 4: Frontend Types & API Client

**Files:**
- Modify: `frontend/src/api/types.ts`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add missing fields to Service type**

In `frontend/src/api/types.ts`, add to the ContainerSpec object in the Service interface (after existing fields like `ReadOnly`):

```ts
TTY?: boolean;
Groups?: string[];
Hosts?: string[];
DNSConfig?: {
  Nameservers?: string[];
  Search?: string[];
  Options?: string[];
};
CapabilityAdd?: string[];
CapabilityDrop?: string[];
```

- [ ] **Step 2: Add ContainerConfig type**

Add to `frontend/src/api/types.ts` as a new exported interface:

```ts
export interface ContainerConfig {
  command?: string[];
  args?: string[];
  dir: string;
  user: string;
  hostname: string;
  init?: boolean;
  tty: boolean;
  readOnly: boolean;
  stopSignal: string;
  stopGracePeriod?: number;
  capabilityAdd?: string[];
  capabilityDrop?: string[];
  groups?: string[];
  hosts?: string[];
  dnsConfig?: {
    nameservers?: string[];
    search?: string[];
    options?: string[];
  };
}
```

- [ ] **Step 3: Add API methods**

In `frontend/src/api/client.ts`, add in the tier-2 sub-resource GETs section:

```ts
serviceContainerConfig: (id: string, signal?: AbortSignal) =>
  fetchJSON<ContainerConfig>(`/services/${id}/container-config`, signal),
```

Add in the tier-2 PATCH section (after `patchServiceLogDriver` or similar):

```ts
patchServiceContainerConfig: (id: string, partial: Record<string, unknown>) =>
  patch<ContainerConfig>(`/services/${id}/container-config`, partial, "application/merge-patch+json"),
```

Note: use `Record<string, unknown>` for the patch body rather than `Partial<ContainerConfig>` because merge-patches can include `null` values to clear fields, which `Partial<ContainerConfig>` doesn't express.

- [ ] **Step 4: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/types.ts frontend/src/api/client.ts
git commit -m "feat: add ContainerConfig type and API methods"
```

---

### Task 5: CommandEditor Component

**Files:**
- Create: `frontend/src/components/service-detail/CommandEditor.tsx`

This editor handles `command`, `args`, `dir`, `user`. Follow the `HealthcheckEditor` pattern exactly: display/edit toggle, `useEscapeCancel`, manual `useState` for saving/error, Edit button in section header via `controls` prop on a bordered `div` (not a CollapsibleSection — this editor lives inside the parent Container Configuration section).

- [ ] **Step 1: Create CommandEditor**

Create `frontend/src/components/service-detail/CommandEditor.tsx`. The component should:

- Accept props: `serviceId: string`, `config: ContainerConfig`, `onSaved: (updated: ContainerConfig) => void`
- Display mode: show Command, Args, Working Dir, User as label/value pairs (using a simple grid or KV layout). Use monospace for Command and Args. Show "—" for empty/undefined.
- Edit mode: four `<input>` fields. Command and Args are single text inputs where the user types space-separated tokens (displayed as `["/bin/sh", "-c"]` → `/bin/sh -c`). Dir and User are plain text inputs.
- On save: split Command and Args strings on spaces into `string[]`, call `api.patchServiceContainerConfig(serviceId, { command, args, dir, user })`.
- Gate edit button behind `operationsLevel >= opsLevel.configuration`.
- Use `useEscapeCancel(editing, cancelEdit)`.
- Manual `saving`/`saveError` state with try/catch/finally (not `useAsyncAction`).

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/CommandEditor.tsx
git commit -m "feat: add CommandEditor component"
```

---

### Task 6: RuntimeEditor Component

**Files:**
- Create: `frontend/src/components/service-detail/RuntimeEditor.tsx`

Handles `hostname`, `init`, `tty`, `readOnly`, `stopSignal`, `stopGracePeriod`.

- [ ] **Step 1: Create RuntimeEditor**

Create `frontend/src/components/service-detail/RuntimeEditor.tsx`. The component should:

- Accept same props pattern as CommandEditor.
- Display mode: Hostname, Init (Yes/No/Default), TTY (Yes/No), Read Only (Yes/No), Stop Signal, Stop Grace Period (convert nanoseconds to human-readable, e.g. "10s", "500ms").
- Edit mode: Hostname and Stop Signal are text inputs. Init, TTY, Read Only are `<input type="checkbox">` elements. Stop Grace Period is a numeric input labeled "seconds" — multiply by 1e9 on save, divide by 1e9 on load.
- Init has three states: `undefined` (Docker default — unchecked with "(default)" label), `true` (checked), `false` (unchecked). A "Reset to default" link appears when Init has been explicitly set.
- On save: call `api.patchServiceContainerConfig(serviceId, { hostname, init, tty, readOnly, stopSignal, stopGracePeriod })`.
- Same gating and state patterns as CommandEditor.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/RuntimeEditor.tsx
git commit -m "feat: add RuntimeEditor component"
```

---

### Task 7: CapabilitiesEditor Component

**Files:**
- Create: `frontend/src/components/service-detail/CapabilitiesEditor.tsx`

Handles `capabilityAdd`, `capabilityDrop`.

- [ ] **Step 1: Create CapabilitiesEditor**

Create `frontend/src/components/service-detail/CapabilitiesEditor.tsx`. The component should:

- Accept same props pattern.
- Display mode: Two labeled groups — "Add" and "Drop". Each shows capabilities as inline badges (small rounded `<span>` elements). "None" if empty/undefined.
- Edit mode: Two sections, each with a text input + Enter-to-add pattern. Typed text is auto-uppercased. Each capability appears as a badge with an × remove button. Input field clears after adding.
- On save: call `api.patchServiceContainerConfig(serviceId, { capabilityAdd, capabilityDrop })`.
- Same gating and state patterns.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/CapabilitiesEditor.tsx
git commit -m "feat: add CapabilitiesEditor component"
```

---

### Task 8: ExtraHostsEditor Component

**Files:**
- Create: `frontend/src/components/service-detail/ExtraHostsEditor.tsx`

Handles `hosts`.

- [ ] **Step 1: Create ExtraHostsEditor**

Create `frontend/src/components/service-detail/ExtraHostsEditor.tsx`. The component should:

- Accept same props pattern.
- Display mode: Table with IP Address and Hostname columns. Parse Docker's `/etc/hosts` format: `"IP_address hostname [aliases...]"` (space-separated, IP first). "None" if empty/undefined.
- Edit mode: Editable rows — each row has IP input and hostname input. Add Row button appends a blank row. Remove button (× or trash icon) deletes a row. On save, join each row back to `"IP hostname"` strings (space-separated, IP first — matching the Docker swarmkit format) and call `api.patchServiceContainerConfig(serviceId, { hosts })`.
- Same gating and state patterns.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/ExtraHostsEditor.tsx
git commit -m "feat: add ExtraHostsEditor component"
```

---

### Task 9: DnsEditor Component

**Files:**
- Create: `frontend/src/components/service-detail/DnsEditor.tsx`

Handles `dnsConfig`.

- [ ] **Step 1: Create DnsEditor**

Create `frontend/src/components/service-detail/DnsEditor.tsx`. The component should:

- Accept same props pattern.
- Display mode: Three labeled sub-sections — Nameservers, Search Domains, Options. Each shows values comma-separated. "Default" if `dnsConfig` is undefined/null.
- Edit mode: Three text inputs for comma-separated values. The component splits on commas (trimming whitespace) to produce `string[]` on save. Empty input → undefined for that field. All three empty → send `dnsConfig: null` to clear.
- On save: call `api.patchServiceContainerConfig(serviceId, { dnsConfig: { nameservers, search, options } })` or `{ dnsConfig: null }` if all empty.
- Same gating and state patterns.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/DnsEditor.tsx
git commit -m "feat: add DnsEditor component"
```

---

### Task 10: Integrate into ServiceDetail Page

**Files:**
- Modify: `frontend/src/components/service-detail/index.ts`
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Add barrel exports**

In `frontend/src/components/service-detail/index.ts`, add:

```ts
export { CommandEditor } from "./CommandEditor";
export { RuntimeEditor } from "./RuntimeEditor";
export { CapabilitiesEditor } from "./CapabilitiesEditor";
export { ExtraHostsEditor } from "./ExtraHostsEditor";
export { DnsEditor } from "./DnsEditor";
```

- [ ] **Step 2: Add imports and state in ServiceDetail.tsx**

Import the new editors from the barrel. Import `ContainerConfig` from `../api/types`.

Add state:
```ts
const [containerConfig, setContainerConfig] = useState<ContainerConfig | null>(null);
```

Add fetch in the data loading section:
```ts
api
  .serviceContainerConfig(id!, signal)
  .then(setContainerConfig)
  .catch(() => {});
```

- [ ] **Step 3: Replace Container Configuration section**

Replace the existing read-only `KVTable` inside the "Container Configuration" `CollapsibleSection` with the five editors in a two-column grid:

```tsx
<CollapsibleSection
  title="Container Configuration"
  defaultOpen={containerConfig != null}
>
  {containerConfig ? (
    <div className="grid gap-4 sm:grid-cols-2">
      <CommandEditor
        serviceId={id!}
        config={containerConfig}
        onSaved={setContainerConfig}
      />
      <RuntimeEditor
        serviceId={id!}
        config={containerConfig}
        onSaved={setContainerConfig}
      />
      <CapabilitiesEditor
        serviceId={id!}
        config={containerConfig}
        onSaved={setContainerConfig}
      />
      <ExtraHostsEditor
        serviceId={id!}
        config={containerConfig}
        onSaved={setContainerConfig}
      />
      <DnsEditor
        serviceId={id!}
        config={containerConfig}
        onSaved={setContainerConfig}
      />
    </div>
  ) : (
    <div className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">
      Loading…
    </div>
  )}
</CollapsibleSection>
```

- [ ] **Step 4: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 5: Verify lint and format**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint && npm run fmt`

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/service-detail/index.ts frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: integrate container config editors into service detail page"
```

---

### Task 11: Full Verification

**Files:** None (verification only)

- [ ] **Step 1: Run all backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`

- [ ] **Step 2: Run frontend checks**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npm run lint && npm run fmt:check`

- [ ] **Step 3: Run full make check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make check`
Expected: All checks pass
