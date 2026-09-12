# Deduplication Refactor

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce mechanical duplication across backend handlers and frontend pages/hooks with simple function extractions — no new features, no behavior changes.

**Architecture:** Each task extracts a helper (function or component) from repeated code, then replaces all call sites. All changes are purely mechanical; existing tests must pass unchanged.

**Tech Stack:** Go 1.22+, React 19, TypeScript

---

### Task 1: `sortColumn` helper (Frontend)

**Files:**
- Create: `frontend/src/lib/sortColumn.tsx`
- Modify: `frontend/src/pages/ConfigList.tsx`
- Modify: `frontend/src/pages/SecretList.tsx`
- Modify: `frontend/src/pages/NetworkList.tsx`
- Modify: `frontend/src/pages/VolumeList.tsx`
- Modify: `frontend/src/pages/NodeList.tsx`
- Modify: `frontend/src/pages/ServiceList.tsx`
- Modify: `frontend/src/pages/TaskList.tsx`

- [ ] **Step 1: Create `sortColumn` helper**

```tsx
// frontend/src/lib/sortColumn.tsx
import type { ReactNode } from "react";
import SortIndicator from "../components/SortIndicator";
import type { SortDir } from "../hooks/useSort";

/**
 * Returns `header` and `onHeaderClick` props for a sortable DataTable column.
 */
export function sortColumn(
  label: string,
  key: string,
  sortKey: string | undefined,
  sortDir: SortDir,
  toggle: (key: string) => void,
): { header: ReactNode; onHeaderClick: () => void } {
  return {
    header: (
      <SortIndicator
        label={label}
        active={sortKey === key}
        dir={sortDir}
      />
    ),
    onHeaderClick: () => toggle(key),
  };
}
```

- [ ] **Step 2: Replace in `ConfigList.tsx`**

Replace each column's `header` + `onHeaderClick` with spread of `sortColumn(...)`. For example, the Name column becomes:

```tsx
{
  ...sortColumn("Name", "name", sortKey, sortDir, toggle),
  cell: ({ ID, Spec: { Name } }) => <ResourceName name={Name || ID} />,
},
```

Remove the `SortIndicator` import. Apply the same pattern to all 3 columns.

- [ ] **Step 3: Replace in remaining 6 list pages**

Apply the same transformation to `SecretList.tsx`, `NetworkList.tsx`, `VolumeList.tsx`, `NodeList.tsx`, `ServiceList.tsx`, `TaskList.tsx`. Each sorted column that currently has inline `<SortIndicator>` + `onHeaderClick` gets replaced with `...sortColumn(...)`.

- [ ] **Step 4: Verify**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

- [ ] **Step 5: Commit**

```
refactor(frontend): extract sortColumn helper to deduplicate sorted column definitions
```

---

### Task 2: `requireMergePatch` helper (Backend)

**Files:**
- Modify: `internal/api/write_helpers.go`
- Modify: `internal/api/write_service_handlers.go`

- [ ] **Step 1: Add `requireMergePatch` to `write_helpers.go`**

Add after `applyStructMergePatch`:

```go
// requireMergePatch validates Content-Type is application/merge-patch+json.
// Returns false and writes a 415 error response if not satisfied.
func requireMergePatch(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/merge-patch+json") {
		writeErrorCode(w, r, "API004", "expected Content-Type: application/merge-patch+json")
		return false
	}
	return true
}
```

Add `"strings"` to the import if not already present.

- [ ] **Step 2: Replace all inline guards in `write_service_handlers.go`**

In every PATCH handler that checks for merge-patch only (`HandlePatchServiceResources`, `HandlePatchServicePorts`, `HandlePatchServiceUpdatePolicy`, `HandlePatchServiceRollbackPolicy`, `HandlePatchServiceLogDriver`, `HandlePatchServiceHealthcheck`, `HandlePatchServiceContainerConfig`, `HandlePatchServiceConfigs`, `HandlePatchServiceSecrets`, `HandlePatchServiceNetworks`, `HandlePatchServiceMounts`), replace the 8-line block:

```go
ct := r.Header.Get("Content-Type")
if !strings.HasPrefix(ct, "application/merge-patch+json") {
	writeErrorCode(
		w,
		r,
		"API004",
		"expected Content-Type: application/merge-patch+json",
	)
	return
}
```

with:

```go
if !requireMergePatch(w, r) {
	return
}
```

If `strings` is no longer used in `write_service_handlers.go` after this change, remove it from the imports.

- [ ] **Step 3: Verify**

Run: `go build ./... && go test ./internal/api/...`

- [ ] **Step 4: Commit**

```
refactor(api): extract requireMergePatch helper
```

---

### Task 3: `patchStringMap` helper (Backend)

**Files:**
- Modify: `internal/api/write_helpers.go`
- Modify: `internal/api/write_service_handlers.go` (HandlePatchServiceEnv, HandlePatchServiceLabels)
- Modify: `internal/api/write_node_handlers.go` (HandlePatchNodeLabels)
- Modify: `internal/api/write_config_handlers.go` (HandlePatchConfigLabels)
- Modify: `internal/api/write_secret_handlers.go` (HandlePatchSecretLabels)

- [ ] **Step 1: Add `patchStringMap` to `write_helpers.go`**

```go
// patchStringMap reads a JSON Patch or Merge Patch body, applies it to
// current, and returns the updated map. Returns nil and false (and writes
// the error response) on any failure.
func patchStringMap(
	w http.ResponseWriter,
	r *http.Request,
	current map[string]string,
) (map[string]string, bool) {
	ct := r.Header.Get("Content-Type")
	isJSONPatch := strings.HasPrefix(ct, "application/json-patch+json")
	isMergePatch := strings.HasPrefix(ct, "application/merge-patch+json")

	if !isJSONPatch && !isMergePatch {
		writeErrorCode(
			w,
			r,
			"API004",
			"Content-Type must be application/json-patch+json or application/merge-patch+json",
		)
		return nil, false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return nil, false
	}

	if current == nil {
		current = map[string]string{}
	}

	var updated map[string]string
	if isJSONPatch {
		var ops []PatchOp
		if err := json.Unmarshal(body, &ops); err != nil {
			writeErrorCode(w, r, "API006", "invalid request body")
			return nil, false
		}
		updated, err = applyJSONPatch(current, ops)
	} else {
		updated, err = applyMergePatchStringMap(current, body)
	}

	if err != nil {
		writePatchError(w, r, err)
		return nil, false
	}

	return updated, true
}
```

- [ ] **Step 2: Replace `HandlePatchServiceLabels` in `write_service_handlers.go`**

Replace the body after `r.Body = http.MaxBytesReader(...)` and after lookup with:

```go
func (h *Handlers) HandlePatchServiceLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", fmt.Sprintf("service %q not found", id))
		return
	}

	updated, ok := patchStringMap(w, r, svc.Spec.Labels)
	if !ok {
		return
	}

	slog.Info("patching service labels", "service", id)

	result, err := h.serviceSpec.UpdateServiceLabels(r.Context(), id, updated)
	if err != nil {
		writeServiceError(w, r, err, id)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeMutationResponse(w, r, labels)
}
```

- [ ] **Step 3: Replace `HandlePatchServiceEnv`**

Same pattern, but the current map comes from `envSliceToMap(env)` not directly from spec labels. The handler still needs its env-specific lookup and response:

```go
func (h *Handlers) HandlePatchServiceEnv(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", fmt.Sprintf("service %q not found", id))
		return
	}

	var env []string
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		env = svc.Spec.TaskTemplate.ContainerSpec.Env
	}

	updated, ok := patchStringMap(w, r, envSliceToMap(env))
	if !ok {
		return
	}

	slog.Info("patching service env", "service", id)

	result, err := h.serviceSpec.UpdateServiceEnv(r.Context(), id, updated)
	if err != nil {
		writeServiceError(w, r, err, id)
		return
	}

	var resultEnv []string
	if result.Spec.TaskTemplate.ContainerSpec != nil {
		resultEnv = result.Spec.TaskTemplate.ContainerSpec.Env
	}
	writeMutationResponse(w, r, envSliceToMap(resultEnv))
}
```

- [ ] **Step 4: Replace `HandlePatchNodeLabels` in `write_node_handlers.go`**

```go
func (h *Handlers) HandlePatchNodeLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	node, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", fmt.Sprintf("node %q not found", id))
		return
	}

	updated, ok := patchStringMap(w, r, node.Spec.Labels)
	if !ok {
		return
	}

	slog.Info("patching node labels", "node", id)

	result, err := h.nodeWriter.UpdateNodeLabels(r.Context(), id, updated)
	if err != nil {
		writeNodeError(w, r, err, id)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeMutationResponse(w, r, labels)
}
```

- [ ] **Step 5: Replace `HandlePatchConfigLabels` in `write_config_handlers.go`**

```go
func (h *Handlers) HandlePatchConfigLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", fmt.Sprintf("config %q not found", id))
		return
	}

	updated, ok := patchStringMap(w, r, cfg.Spec.Labels)
	if !ok {
		return
	}

	slog.Info("patching config labels", "config", id)

	result, err := h.configWriter.UpdateConfigLabels(r.Context(), id, updated)
	if err != nil {
		writeConfigError(w, r, err, id)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeMutationResponse(w, r, labels)
}
```

- [ ] **Step 6: Replace `HandlePatchSecretLabels` in `write_secret_handlers.go`**

Same pattern as config, using `sec` / `h.secretWriter.UpdateSecretLabels` / `writeSecretError` / `SEC002`.

- [ ] **Step 7: Clean up unused imports**

Remove `io`, `strings`, `json` imports from files that no longer use them directly (the patch logic moved to `write_helpers.go`). Leave imports that are still used by other functions in the same file.

- [ ] **Step 8: Verify**

Run: `go build ./... && go test ./internal/api/...`

- [ ] **Step 9: Commit**

```
refactor(api): extract patchStringMap helper to deduplicate PATCH handlers for string maps
```

---

### Task 4: Shared `parseInstant`/`parseRange` (Frontend)

**Files:**
- Create: `frontend/src/lib/prometheusParser.ts`
- Modify: `frontend/src/hooks/useNodeMetrics.ts`
- Modify: `frontend/src/hooks/useServiceMetrics.ts`

- [ ] **Step 1: Create shared parser**

```ts
// frontend/src/lib/prometheusParser.ts
import type { PrometheusResponse } from "../api/types";

/**
 * Extracts [label, value] pairs from a Prometheus instant query response.
 */
export function parseInstant(
  response: PrometheusResponse | null,
  labelKey: string,
): [string, number][] | null {
  const results = response?.data?.result;

  if (!results?.length) {
    return null;
  }

  return results.map(
    ({ metric, value }) => [metric?.[labelKey] || "", Number(value?.[1])] as [string, number],
  );
}

/**
 * Extracts [label, values[]] pairs from a Prometheus range query response.
 */
export function parseRange(
  response: PrometheusResponse | null,
  labelKey: string,
): [string, number[]][] | null {
  const results = response?.data?.result;

  if (!results?.length) {
    return null;
  }

  return results.map(
    ({ metric, values }) =>
      [metric?.[labelKey] || "", (values || []).map((value) => Number(value[1]))] as [
        string,
        number[],
      ],
  );
}
```

- [ ] **Step 2: Update `useNodeMetrics.ts`**

Remove the local `parseInstant` and `parseRange` functions (lines 138–164). Import from shared module. Replace calls:

```ts
import { parseInstant, parseRange } from "../lib/prometheusParser";

// In the .then callback, replace:
parseInstant(cpuResponse)?.forEach(...)
// with:
parseInstant(cpuResponse, "instance")?.forEach(...)

// Same for all other parseInstant/parseRange calls — add "instance" as labelKey.
```

- [ ] **Step 3: Update `useServiceMetrics.ts`**

Remove the local `parseInstant` and `parseRange` functions (lines 115–141). Import from shared module. Replace calls, using `serviceLabel` as the label key:

```ts
import { parseInstant, parseRange } from "../lib/prometheusParser";

// Replace calls like:
parseInstant(cpuResponse)?.forEach(...)
// with:
parseInstant(cpuResponse, serviceLabel)?.forEach(...)
```

- [ ] **Step 4: Verify**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

- [ ] **Step 5: Commit**

```
refactor(frontend): extract shared Prometheus response parsers
```

---

### Task 5: `hasPrometheus`/`hasCadvisor` derived booleans (Frontend)

**Files:**
- Modify: `frontend/src/hooks/useMonitoringStatus.ts`
- Modify: All files that derive `hasPrometheus`/`hasCadvisor`/`hasNodeExporter` inline

- [ ] **Step 1: Add derived helper functions**

Add to the bottom of `frontend/src/hooks/useMonitoringStatus.ts`:

```ts
/**
 * Derives whether Prometheus is reachable from monitoring status.
 */
export function hasPrometheus(status: MonitoringStatus | null): boolean {
  return !!status?.prometheusConfigured && !!status?.prometheusReachable;
}

/**
 * Derives whether cAdvisor targets are available.
 */
export function hasCadvisor(status: MonitoringStatus | null): boolean {
  return hasPrometheus(status) && !!status?.cadvisor?.targets;
}

/**
 * Derives whether node-exporter targets are available.
 */
export function hasNodeExporter(status: MonitoringStatus | null): boolean {
  return hasPrometheus(status) && !!status?.nodeExporter?.targets;
}
```

Note: This also fixes an inconsistency — some callers checked `hasCadvisor` without verifying Prometheus was reachable. The canonical `hasCadvisor` now always includes the prometheus check.

- [ ] **Step 2: Replace inline derivations across all files**

For each file that computes these locally:

- `pages/NodeList.tsx`: Replace `const hasPrometheus = ...` / `const hasNodeExporter = ...` with imports
- `pages/NodeDetail.tsx`: Replace `const hasPrometheus = ...` / `const hasCadvisor = ...`
- `pages/ServiceList.tsx`: Replace `const hasCadvisor = ...`
- `pages/ServiceDetail.tsx`: Replace `const hasPrometheus = ...` / `const hasCadvisor = ...`
- `pages/TaskList.tsx`: Replace `const hasCadvisor = ...`
- `pages/TaskDetail.tsx`: Replace `const hasCadvisor = ...` / `const hasPrometheus = ...`
- `pages/ClusterOverview.tsx`: Replace `const hasPrometheus = ...`
- `hooks/useNodeMetrics.ts`: Replace `const hasPrometheus = ...`
- `hooks/useServiceMetrics.ts`: Replace `const hasCadvisor = ...`
- `hooks/useInstanceResolver.ts`: Replace `const hasPrometheus = ...`

Each file calls the function passing `monitoring` (the value returned by `useMonitoringStatus()`). For example in `NodeList.tsx`:

```ts
import { useMonitoringStatus, hasPrometheus as checkPrometheus, hasNodeExporter as checkNodeExporter } from "../hooks/useMonitoringStatus";
// ...
const monitoring = useMonitoringStatus();
const hasNodeExporterData = checkNodeExporter(monitoring);
```

Alternatively, since these are called with the same `monitoring` value everywhere, each page can just use the function directly in expressions:

```ts
import { useMonitoringStatus, hasPrometheus, hasCadvisor, hasNodeExporter } from "../hooks/useMonitoringStatus";
// ...
const monitoring = useMonitoringStatus();
// Use: hasPrometheus(monitoring), hasCadvisor(monitoring), hasNodeExporter(monitoring)
```

Choose the approach that reads best at each call site. If the value is used multiple times in the same component, assign to a local `const` with a short name.

- [ ] **Step 3: Verify**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

- [ ] **Step 4: Commit**

```
refactor(frontend): extract hasPrometheus/hasCadvisor/hasNodeExporter helpers

Also fixes inconsistency where some callers checked hasCadvisor
without verifying Prometheus was reachable.
```

---

### Task 6: Grid class constant (Frontend)

**Files:**
- Create: `frontend/src/lib/styles.ts`
- Modify: 9 files that use the grid class string

- [ ] **Step 1: Create constant**

```ts
// frontend/src/lib/styles.ts

/** Responsive 1/2/3-column card grid used by list pages. */
export const cardGridClass = "grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3";
```

- [ ] **Step 2: Replace in all files**

In each of the 9 files (`ConfigList.tsx`, `SecretList.tsx`, `NetworkList.tsx`, `VolumeList.tsx`, `NodeList.tsx`, `ServiceList.tsx`, `StackList.tsx`, `NetworkDetail.tsx`, `ProfilePage.tsx`), import `cardGridClass` and replace:

```tsx
<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
```

with:

```tsx
<div className={cardGridClass}>
```

- [ ] **Step 3: Verify**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

- [ ] **Step 4: Commit**

```
refactor(frontend): extract cardGridClass constant
```

---

### Task 7: `requireMergePatch` in `applyStructMergePatch` (Backend)

**Files:**
- Modify: `internal/api/write_helpers.go`

- [ ] **Step 1: Fold Content-Type check into `applyStructMergePatch`**

Currently, every caller of `applyStructMergePatch` is preceded by a `requireMergePatch` check (after Task 2). Since `applyStructMergePatch` already reads `r.Body`, it's natural for it to also validate the Content-Type. Add the check at the top of `applyStructMergePatch`:

```go
func applyStructMergePatch(
	w http.ResponseWriter,
	r *http.Request,
	current any,
	target any,
	errCode string,
	errMsg string,
) bool {
	if !requireMergePatch(w, r) {
		return false
	}
	// ... rest of existing body
```

Then remove all `requireMergePatch` calls that precede `applyStructMergePatch` calls in `write_service_handlers.go`. The handlers that don't use `applyStructMergePatch` (like `HandlePatchServicePorts` which does its own JSON decode) still keep their standalone `requireMergePatch` call.

- [ ] **Step 2: Verify**

Run: `go build ./... && go test ./internal/api/...`

- [ ] **Step 3: Commit**

```
refactor(api): fold requireMergePatch into applyStructMergePatch
```

---

### Task 8: Verify all tests pass end-to-end

- [ ] **Step 1: Run full backend test suite**

Run: `go test ./...`

- [ ] **Step 2: Run full frontend test + lint suite**

Run: `cd frontend && npx vitest run && npm run lint && npx tsc -b --noEmit`

- [ ] **Step 3: Fix any failures**

If any tests fail, investigate and fix. All changes are mechanical — failures indicate a mistake in the replacement, not a design issue.
