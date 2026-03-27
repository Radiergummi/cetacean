# Label Integrations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect well-known label-based integrations (starting with Traefik) on Docker Swarm services and return structured configuration in the API, rendered as dedicated panels in the UI.

**Architecture:** New `internal/integrations/` Go package with per-tool detectors. `Detect()` runs at request time in the service detail handler, returning typed integration objects as extras in the JSON-LD response. Frontend renders one `CollapsibleSection` per detected integration above the labels editor, filtering consumed keys from the raw labels view.

**Tech Stack:** Go (backend parsing, table-driven tests), React + TypeScript (frontend panels), Tailwind/shadcn (styling)

**Spec:** `docs/superpowers/specs/2026-03-27-label-integrations-design.md`

---

## File Structure

### Backend (new files)
- `internal/integrations/integrations.go` — `Detect()` entry point, `Result` type
- `internal/integrations/integrations_test.go` — Tests for `Detect()` orchestration
- `internal/integrations/traefik.go` — Traefik types and parser
- `internal/integrations/traefik_test.go` — Table-driven Traefik parser tests

### Backend (modified files)
- `internal/api/handlers.go:904-918` — Call `Detect()` in `HandleGetService`
- `internal/api/handlers_test.go:702-717` — Add test for integrations in service detail response

### Frontend (new files)
- `frontend/src/components/service-detail/TraefikPanel.tsx` — Traefik integration panel

### Frontend (modified files)
- `frontend/src/api/types.ts:332-335` — Add integration types, extend `ServiceDetail`
- `frontend/src/pages/ServiceDetail.tsx:448-465` — Render integration panels, filter labels

### API docs (modified files)
- `api/openapi.yaml` — Add integration schemas to service detail response

---

## Task 1: Traefik label parser — types and core parsing

**Files:**
- Create: `internal/integrations/traefik.go`
- Create: `internal/integrations/traefik_test.go`

- [ ] **Step 1: Write failing test for basic router parsing**

In `internal/integrations/traefik_test.go`:

```go
package integrations

import (
	"testing"
)

func TestDetectTraefik_BasicRouter(t *testing.T) {
	labels := map[string]string{
		"traefik.enable":                                        "true",
		"traefik.http.routers.myapp.rule":                       "Host(`example.com`)",
		"traefik.http.routers.myapp.entrypoints":                "websecure",
		"traefik.http.routers.myapp.tls.certresolver":           "letsencrypt",
		"traefik.http.routers.myapp.middlewares":                 "auth,ratelimit",
		"traefik.http.routers.myapp.service":                    "myapp",
		"traefik.http.services.myapp.loadbalancer.server.port":  "8080",
		"traefik.http.services.myapp.loadbalancer.server.scheme": "http",
		"traefik.http.middlewares.auth.basicauth.users":          "admin:$$apr1$$...",
	}

	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected traefik integration, got nil")
	}

	if !result.Enabled {
		t.Error("expected enabled=true")
	}

	if len(result.Routers) != 1 {
		t.Fatalf("expected 1 router, got %d", len(result.Routers))
	}
	r := result.Routers[0]
	if r.Name != "myapp" {
		t.Errorf("router name: got %q, want %q", r.Name, "myapp")
	}
	if r.Rule != "Host(`example.com`)" {
		t.Errorf("router rule: got %q", r.Rule)
	}
	if len(r.Entrypoints) != 1 || r.Entrypoints[0] != "websecure" {
		t.Errorf("router entrypoints: got %v", r.Entrypoints)
	}
	if r.TLS == nil || r.TLS.CertResolver != "letsencrypt" {
		t.Errorf("router TLS: got %+v", r.TLS)
	}
	if len(r.Middlewares) != 2 || r.Middlewares[0] != "auth" || r.Middlewares[1] != "ratelimit" {
		t.Errorf("router middlewares: got %v", r.Middlewares)
	}
	if r.Service != "myapp" {
		t.Errorf("router service: got %q", r.Service)
	}

	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}
	s := result.Services[0]
	if s.Name != "myapp" {
		t.Errorf("service name: got %q", s.Name)
	}
	if s.Port != 8080 {
		t.Errorf("service port: got %d", s.Port)
	}
	if s.Scheme != "http" {
		t.Errorf("service scheme: got %q", s.Scheme)
	}

	if len(result.Middlewares) != 1 {
		t.Fatalf("expected 1 middleware, got %d", len(result.Middlewares))
	}
	m := result.Middlewares[0]
	if m.Name != "auth" {
		t.Errorf("middleware name: got %q", m.Name)
	}
	if m.Type != "basicauth" {
		t.Errorf("middleware type: got %q", m.Type)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrations/ -run TestDetectTraefik_BasicRouter -v`
Expected: compilation error (package/types don't exist yet)

- [ ] **Step 3: Implement Traefik types and parser**

Create `internal/integrations/traefik.go` with:

- Types: `TraefikIntegration`, `TraefikRouter`, `TraefikTLS`, `TraefikTLSDomain`, `TraefikService`, `TraefikMiddleware`
- `detectTraefik(labels map[string]string) *TraefikIntegration` — returns nil if no `traefik.` labels found
- Parser logic:
  - Iterate labels, collect all with `traefik.` prefix
  - `traefik.enable` → `Enabled` field (default true if any traefik labels present but no explicit enable)
  - `traefik.http.routers.<name>.<field>` → group by name into `TraefikRouter`
  - `traefik.http.services.<name>.loadbalancer.server.port` → `TraefikService.Port`
  - `traefik.http.services.<name>.loadbalancer.server.scheme` → `TraefikService.Scheme`
  - `traefik.http.middlewares.<name>.<type>.<field>` → group by name, first segment after name is the type
  - Entrypoints and middlewares are comma-separated strings → split into `[]string`
  - TLS fields: `tls.certresolver`, `tls.options`, `tls.domains[N].main`, `tls.domains[N].sans`
  - `priority` → parse as int
  - Sort routers, services, and middlewares by name before returning (deterministic output for stable ETags)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/integrations/ -run TestDetectTraefik_BasicRouter -v`
Expected: PASS

- [ ] **Step 5: Add edge case tests**

Add to `internal/integrations/traefik_test.go`:

```go
func TestDetectTraefik_NoLabels(t *testing.T) {
	labels := map[string]string{
		"com.docker.stack.namespace": "mystack",
		"other.label":               "value",
	}
	result := detectTraefik(labels)
	if result != nil {
		t.Error("expected nil when no traefik labels")
	}
}

func TestDetectTraefik_EnabledFalse(t *testing.T) {
	labels := map[string]string{
		"traefik.enable": "false",
	}
	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected integration even when disabled")
	}
	if result.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestDetectTraefik_MultipleRouters(t *testing.T) {
	labels := map[string]string{
		"traefik.http.routers.web.rule":    "Host(`web.example.com`)",
		"traefik.http.routers.api.rule":    "Host(`api.example.com`)",
		"traefik.http.routers.api.service": "api-svc",
	}
	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected integration")
	}
	if len(result.Routers) != 2 {
		t.Fatalf("expected 2 routers, got %d", len(result.Routers))
	}
}

func TestDetectTraefik_TLSDomains(t *testing.T) {
	labels := map[string]string{
		"traefik.http.routers.myapp.tls.domains[0].main": "example.com",
		"traefik.http.routers.myapp.tls.domains[0].sans": "*.example.com,www.example.com",
	}
	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected integration")
	}
	if len(result.Routers) != 1 {
		t.Fatalf("expected 1 router, got %d", len(result.Routers))
	}
	r := result.Routers[0]
	if r.TLS == nil || len(r.TLS.Domains) != 1 {
		t.Fatalf("expected 1 TLS domain, got %+v", r.TLS)
	}
	d := r.TLS.Domains[0]
	if d.Main != "example.com" {
		t.Errorf("domain main: got %q", d.Main)
	}
	if len(d.SANs) != 2 {
		t.Errorf("domain SANs: got %v", d.SANs)
	}
}

func TestDetectTraefik_MiddlewareConfig(t *testing.T) {
	labels := map[string]string{
		"traefik.http.middlewares.redir.redirectscheme.scheme":    "https",
		"traefik.http.middlewares.redir.redirectscheme.permanent": "true",
	}
	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected integration")
	}
	if len(result.Middlewares) != 1 {
		t.Fatalf("expected 1 middleware, got %d", len(result.Middlewares))
	}
	m := result.Middlewares[0]
	if m.Type != "redirectscheme" {
		t.Errorf("middleware type: got %q", m.Type)
	}
	if m.Config["scheme"] != "https" {
		t.Errorf("middleware config scheme: got %q", m.Config["scheme"])
	}
	if m.Config["permanent"] != "true" {
		t.Errorf("middleware config permanent: got %q", m.Config["permanent"])
	}
}

func TestDetectTraefik_TCPLabelsConsumed(t *testing.T) {
	labels := map[string]string{
		"traefik.tcp.routers.db.rule": "HostSNI(`db.example.com`)",
		"traefik.tcp.routers.db.tls":  "true",
	}
	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected integration for tcp labels")
	}
	if len(result.Routers) != 0 {
		t.Errorf("expected 0 HTTP routers, got %d", len(result.Routers))
	}
}

func TestDetectTraefik_RouterWithoutService(t *testing.T) {
	labels := map[string]string{
		"traefik.http.routers.web.rule": "Host(`example.com`)",
	}
	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected integration")
	}
	if len(result.Routers) != 1 {
		t.Fatalf("expected 1 router, got %d", len(result.Routers))
	}
	if len(result.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(result.Services))
	}
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/integrations/ -v`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/integrations/traefik.go internal/integrations/traefik_test.go
git commit -m "feat: add Traefik label parser"
```

---

## Task 2: Detect() orchestrator and consumed-key tracking

**Files:**
- Create: `internal/integrations/integrations.go`
- Create: `internal/integrations/integrations_test.go`

- [ ] **Step 1: Write failing test for Detect()**

In `internal/integrations/integrations_test.go`:

```go
package integrations

import "testing"

func TestDetect_NoIntegrations(t *testing.T) {
	labels := map[string]string{
		"com.docker.stack.namespace": "mystack",
		"app.version":               "1.0",
	}
	result := Detect(labels)
	if len(result.Integrations) != 0 {
		t.Errorf("expected no integrations, got %d", len(result.Integrations))
	}
	if len(result.Remaining) != 2 {
		t.Errorf("expected 2 remaining labels, got %d", len(result.Remaining))
	}
}

func TestDetect_TraefikDetected(t *testing.T) {
	labels := map[string]string{
		"traefik.enable":                                       "true",
		"traefik.http.routers.web.rule":                        "Host(`example.com`)",
		"traefik.http.services.web.loadbalancer.server.port":   "8080",
		"com.docker.stack.namespace":                           "mystack",
		"app.version":                                          "1.0",
	}
	result := Detect(labels)
	if len(result.Integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(result.Integrations))
	}
	if len(result.Remaining) != 2 {
		t.Errorf("expected 2 remaining labels, got %d: %v", len(result.Remaining), result.Remaining)
	}
	if _, ok := result.Remaining["traefik.enable"]; ok {
		t.Error("traefik labels should not be in remaining")
	}
}

func TestDetect_TCPLabelsConsumed(t *testing.T) {
	labels := map[string]string{
		"traefik.tcp.routers.db.rule": "HostSNI(`db.example.com`)",
		"app.version":                 "1.0",
	}
	result := Detect(labels)
	if len(result.Integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(result.Integrations))
	}
	if len(result.Remaining) != 1 {
		t.Errorf("expected 1 remaining label, got %d: %v", len(result.Remaining), result.Remaining)
	}
	if _, ok := result.Remaining["traefik.tcp.routers.db.rule"]; ok {
		t.Error("traefik.tcp labels should not be in remaining")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/integrations/ -run TestDetect -v`
Expected: compilation error (`Detect` not defined)

- [ ] **Step 3: Implement Detect()**

In `internal/integrations/integrations.go`:

```go
package integrations

import "strings"

// Result holds detected integrations for a service.
type Result struct {
	Integrations []any             `json:"integrations,omitempty"`
	Remaining    map[string]string `json:"-"`
}

// Detect runs all registered detectors against the given labels.
// Returns detected integrations and the remaining unconsumed labels.
func Detect(labels map[string]string) Result {
	remaining := make(map[string]string, len(labels))
	for k, v := range labels {
		remaining[k] = v
	}

	var integrations []any

	if t := detectTraefik(labels); t != nil {
		integrations = append(integrations, t)
		for k := range labels {
			if strings.HasPrefix(k, "traefik.") {
				delete(remaining, k)
			}
		}
	}

	return Result{
		Integrations: integrations,
		Remaining:    remaining,
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/integrations/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/integrations/integrations.go internal/integrations/integrations_test.go
git commit -m "feat: add integration detection orchestrator"
```

---

## Task 3: Wire into service detail handler

**Files:**
- Modify: `internal/api/handlers.go:904-918`
- Modify: `internal/api/handlers_test.go` (add new test)

- [ ] **Step 1: Write failing test for integrations in response**

Add to `internal/api/handlers_test.go`:

```go
func TestHandleGetService_WithTraefikIntegration(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.Labels = map[string]string{
		"traefik.enable":                                      "true",
		"traefik.http.routers.web.rule":                       "Host(`example.com`)",
		"traefik.http.services.web.loadbalancer.server.port":  "8080",
		"app.version":                                         "1.0",
	}
	c.SetService(svc)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["integrations"]; !ok {
		t.Error("expected integrations field in response")
	}
}

func TestHandleGetService_NoIntegrationsField(t *testing.T) {
	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "plain"
	svc.Spec.Labels = map[string]string{
		"app.version": "1.0",
	}
	c.SetService(svc)
	h := NewHandlers(c, nil, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)

	req := httptest.NewRequest("GET", "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := body["integrations"]; ok {
		t.Error("expected no integrations field when none detected")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestHandleGetService_WithTraefik -v`
Expected: FAIL (no integrations field in response)

- [ ] **Step 3: Wire Detect() into HandleGetService**

In `internal/api/handlers.go`, modify `HandleGetService` (line 904-918):

```go
func (h *Handlers) HandleGetService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", fmt.Sprintf("service %q not found", id))
		return
	}
	extra := map[string]any{
		"service": svc,
	}
	if changes := DiffServiceSpecs(svc.PreviousSpec, &svc.Spec); len(changes) > 0 {
		extra["changes"] = changes
	}
	if result := integrations.Detect(svc.Spec.Labels); len(result.Integrations) > 0 {
		extra["integrations"] = result.Integrations
	}
	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id, "Service", extra))
}
```

Add import: `"github.com/radiergummi/cetacean/internal/integrations"`

- [ ] **Step 4: Run tests**

Run: `go test ./internal/api/ -run TestHandleGetService -v`
Expected: all PASS

- [ ] **Step 5: Run full backend test suite**

Run: `go test ./...`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/api/handlers_test.go
git commit -m "feat: wire integration detection into service detail endpoint"
```

---

## Task 4: Frontend types

**Files:**
- Modify: `frontend/src/api/types.ts:332-335`

- [ ] **Step 1: Add integration types**

Add after the `ServiceRef` interface (around line 330) in `frontend/src/api/types.ts`:

```typescript
export interface TraefikTLSDomain {
  main: string;
  sans?: string[];
}

export interface TraefikRouter {
  name: string;
  rule?: string;
  entrypoints?: string[];
  tls?: {
    certResolver?: string;
    domains?: TraefikTLSDomain[];
    options?: string;
  };
  middlewares?: string[];
  service?: string;
  priority?: number;
}

export interface TraefikService {
  name: string;
  port?: number;
  scheme?: string;
}

export interface TraefikMiddleware {
  name: string;
  type: string;
  config?: Record<string, string>;
}

export interface TraefikIntegration {
  name: "traefik";
  enabled: boolean;
  routers?: TraefikRouter[];
  services?: TraefikService[];
  middlewares?: TraefikMiddleware[];
}

export type Integration = TraefikIntegration;
```

Then extend `ServiceDetail` (line 332-335):

```typescript
export interface ServiceDetail {
  service: Service;
  changes?: SpecChange[];
  integrations?: Integration[];
}
```

- [ ] **Step 2: Type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/types.ts
git commit -m "feat: add integration types for service detail"
```

---

## Task 5: Traefik panel component

**Files:**
- Create: `frontend/src/components/service-detail/TraefikPanel.tsx`

- [ ] **Step 1: Create the TraefikPanel component**

Create `frontend/src/components/service-detail/TraefikPanel.tsx`. The component should:

- Accept a `TraefikIntegration` prop
- Render inside a `CollapsibleSection` titled "Traefik" (defaultOpen when enabled)
- Show an "enabled: false" muted badge when `!enabled`
- **Routers section**: For each router, render as a compact row/card:
  - Name in bold
  - Rule in monospace
  - Entrypoints as small badges
  - TLS indicator (lock icon or badge) with cert resolver name
  - Middleware names as badges
  - Target service name
- **Services section**: For each service, show name, port, scheme
- **Middlewares section**: For each middleware, show name, type badge, config entries as key-value pills

Use the same visual language as existing service detail sections (see `MountsEditor.tsx` for the badge/pill pattern, `PortsEditor.tsx` for the compact row layout). Import `CollapsibleSection` from `@/components/CollapsibleSection`. Use `KeyValuePills` from `@/components/data/KeyValuePills` for middleware config display.

Keep it simple — no editing, no state, pure rendering.

- [ ] **Step 2: Type check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/service-detail/TraefikPanel.tsx
git commit -m "feat: add Traefik integration panel component"
```

---

## Task 6: Wire into ServiceDetail page

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx:448-465`

- [ ] **Step 1: Add integrations state and pass through**

In `ServiceDetail.tsx`:

1. Add imports:
   - `import TraefikPanel from "@/components/service-detail/TraefikPanel";`
   - `import type { Integration } from "@/api/types";`

2. Add state declaration (alongside existing state at line 64):
   ```typescript
   const [integrations, setIntegrations] = useState<Integration[]>([]);
   ```

3. Populate from fetch response — in `fetchService` callback (line 104-107), add:
   ```typescript
   setIntegrations(response.integrations ?? []);
   ```

4. Above the Labels `KeyValueEditor` (line 448), render integration panels:

```tsx
{integrations?.map((integration) => {
  switch (integration.name) {
    case "traefik":
      return <TraefikPanel key={integration.name} integration={integration} />;
    default:
      return null;
  }
})}
```

4. Filter consumed label keys from the labels editor. Before passing `serviceLabels` to `KeyValueEditor`, filter out keys based on detected integrations:

```typescript
const filteredLabels = useMemo(() => {
  if (!serviceLabels) {
    return null;
  }

  const prefixes: string[] = [];

  for (const integration of integrations ?? []) {
    if (integration.name === "traefik") {
      prefixes.push("traefik.");
    }
  }

  if (prefixes.length === 0) {
    return serviceLabels;
  }

  const filtered: Record<string, string> = {};

  for (const [key, value] of Object.entries(serviceLabels)) {
    if (!prefixes.some((prefix) => key.startsWith(prefix))) {
      filtered[key] = value;
    }
  }

  return filtered;
}, [serviceLabels, integrations]);
```

Pass `filteredLabels` to `KeyValueEditor` instead of `serviceLabels`. Keep using `serviceLabels` for the `hasLabelsContent` check (so the section still shows if there are only integration labels).

- [ ] **Step 2: Type check and lint**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: render integration panels on service detail page"
```

---

## Task 7: OpenAPI spec update

**Files:**
- Modify: `api/openapi.yaml`

- [ ] **Step 1: Add integration schemas**

Add the Traefik integration schemas to the `components/schemas` section in `api/openapi.yaml`. Add an `integrations` property to the service detail response schema referencing an array of the integration discriminated union.

Use the `name` field as the discriminator. Define schemas for: `TraefikIntegration`, `TraefikRouter`, `TraefikTLS`, `TraefikTLSDomain`, `TraefikService`, `TraefikMiddleware`.

- [ ] **Step 2: Verify the spec is valid YAML**

Run: `cd frontend && npx tsc -b --noEmit` (the Go build also validates the spec is loadable at startup)

- [ ] **Step 3: Commit**

```bash
git add api/openapi.yaml
git commit -m "docs: add integration schemas to OpenAPI spec"
```

---

## Task 8: Changelog

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Add changelog entry**

Add under `[Unreleased]`:

```markdown
### Added
- Detect Traefik configuration from service labels and display as a structured panel on the service detail page
```

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: add changelog entry for label integrations"
```
