# High-Priority Deduplication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the four highest-impact code duplications in the codebase — frontend test boilerplate, two unmigrated list pages, near-identical Go create handlers, and near-identical detail pages.

**Architecture:** Each task is a mechanical extraction — pull repeated code into a shared helper/component, then replace all call sites. No new features, no behavior changes. Existing tests must pass unchanged (except the test files themselves in Task 1).

**Tech Stack:** Go 1.22+, React 19, TypeScript, Vitest

---

### Task 1: Extract shared frontend test utilities

**Files:**
- Create: `frontend/src/test/mocks.ts`
- Modify: `frontend/src/pages/ServiceList.test.tsx`
- Modify: `frontend/src/pages/NodeList.test.tsx`
- Modify: `frontend/src/pages/ConfigList.test.tsx`
- Modify: `frontend/src/pages/SecretList.test.tsx`
- Modify: `frontend/src/pages/NetworkList.test.tsx`
- Modify: `frontend/src/pages/VolumeList.test.tsx`
- Modify: `frontend/src/pages/StackList.test.tsx`
- Modify: `frontend/src/pages/ClusterOverview.test.tsx`
- Modify: `frontend/src/hooks/useSwarmQuery.test.tsx`

- [ ] **Step 1: Create `frontend/src/test/mocks.ts`**

```ts
// frontend/src/test/mocks.ts
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";

/**
 * Minimal EventSource mock for SSE-based tests.
 * Access the most recent instance via MockEventSource.instance.
 */
export class MockEventSource {
  static instance: MockEventSource;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  closed = false;

  constructor(_url: string) {
    MockEventSource.instance = this;
  }

  addEventListener(type: string, handler: (e: MessageEvent) => void) {
    const existing = this.listeners.get(type) || [];
    existing.push(handler);
    this.listeners.set(type, existing);
  }

  close() {
    this.closed = true;
  }

  simulateEvent(type: string, data: unknown) {
    const handlers = this.listeners.get(type) || [];
    const event = new MessageEvent("message", { data: JSON.stringify(data) });
    handlers.forEach((handler) => handler(event));
  }
}

/**
 * Creates a QueryClient configured for tests (no retries, immediate GC).
 */
export function createTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
}

/** Stub localStorage for tests that need it. */
export const localStorageStub = {
  getItem: () => null,
  setItem: vi.fn<() => void>(),
  removeItem: vi.fn<() => void>(),
};

/**
 * Creates a wrapper component for renderHook / render that provides
 * QueryClientProvider and optionally MemoryRouter.
 */
export function createWrapper(
  queryClient: QueryClient,
  options?: { withRouter?: boolean },
): ({ children }: { children: ReactNode }) => ReactNode {
  const withRouter = options?.withRouter ?? true;

  return ({ children }: { children: ReactNode }) => {
    const inner = withRouter ? <MemoryRouter><>{children}</></MemoryRouter> : <>{children}</>;
    return <QueryClientProvider client={queryClient}>{inner}</QueryClientProvider>;
  };
}
```

- [ ] **Step 2: Update `ServiceList.test.tsx`**

Remove the local `MockEventSource` class (lines 9–26), the `testQueryClient` variable, the `beforeEach`/`afterEach` that creates it, and the `wrapper` function. Replace with:

```ts
import { MockEventSource, createTestQueryClient, createWrapper, localStorageStub } from "../test/mocks";

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
});

afterEach(() => {
  vi.restoreAllMocks();
});

const wrapper = () => createWrapper(testQueryClient);
```

Then replace all `{ wrapper }` in `renderHook`/`render` calls with `{ wrapper: wrapper() }`. If the existing code already passes `wrapper` by reference (not calling it), keep that pattern — just point to `createWrapper(testQueryClient)` instead.

- [ ] **Step 3: Update the remaining 6 standard list page tests**

Apply the same transformation to `NodeList.test.tsx`, `ConfigList.test.tsx`, `SecretList.test.tsx`, `NetworkList.test.tsx`, `VolumeList.test.tsx`, `ClusterOverview.test.tsx`. Each follows the identical pattern as ServiceList — delete the local `MockEventSource`, use shared imports.

`ClusterOverview.test.tsx` does NOT stub `localStorage` — omit that line for it.

- [ ] **Step 4: Update `StackList.test.tsx`**

StackList doesn't use `QueryClientProvider`. Delete the local `MockEventSource` and import the shared one. Keep the existing `wrapper` that only uses `MemoryRouter` — don't use `createWrapper` here:

```ts
import { MockEventSource, localStorageStub } from "../test/mocks";

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
  mockSummary.mockReset();
});
```

- [ ] **Step 5: Update `useSwarmQuery.test.tsx`**

Delete the local `MockEventSource` (which includes `simulateEvent`). Import the shared one (which also has `simulateEvent`). Replace `testQueryClient` setup with `createTestQueryClient()`. This test doesn't need a router, so use `createWrapper(testQueryClient, { withRouter: false })`:

```ts
import { MockEventSource, createTestQueryClient, createWrapper } from "../test/mocks";

let testQueryClient: QueryClient;

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  testQueryClient = createTestQueryClient();
});

afterEach(() => {
  testQueryClient.clear();
  vi.restoreAllMocks();
});
```

For the wrapper, use `createWrapper(testQueryClient, { withRouter: false })`.

- [ ] **Step 6: Verify**

Run: `cd frontend && npx vitest run`

All tests must pass. The only changes are import sources — no behavior changes.

- [ ] **Step 7: Commit**

```
refactor(frontend): extract shared test utilities (MockEventSource, createTestQueryClient, createWrapper)
```

---

### Task 2: Add `headerContent` prop to `ResourceListPage` and migrate `ServiceList` + `NodeList`

**Files:**
- Modify: `frontend/src/components/ResourceListPage.tsx`
- Modify: `frontend/src/pages/ServiceList.tsx`
- Modify: `frontend/src/pages/NodeList.tsx`

- [ ] **Step 1: Add `headerContent` prop to `ResourceListPage`**

In `frontend/src/components/ResourceListPage.tsx`, add an optional prop to `ResourceListConfig`:

```ts
interface ResourceListConfig<T> extends UseListPageConfig<T> {
  title: string;
  searchPlaceholder: string;
  itemPath: (item: T) => string;
  columns: (
    sortKey: string | undefined,
    sortDir: SortDir,
    toggle: (key: string) => void,
  ) => Column<T>[];
  renderCard: (item: T) => ReactNode;
  emptyMessage: (hasSearch: boolean) => string;
  actions?: (allowedMethods: Set<string>) => ReactNode;
  skeletonColumns?: number;
  headerContent?: ReactNode;
}
```

Then render it between `PageHeader` and `ListToolbar` in the non-loading branch:

```tsx
return (
  <div>
    <PageHeader
      title={config.title}
      actions={config.actions?.(allowedMethods)}
    />
    {config.headerContent}
    <ListToolbar
      // ... existing props
    />
    {/* ... rest unchanged */}
  </div>
);
```

- [ ] **Step 2: Verify no regressions**

Run: `cd frontend && npx vitest run`

This is an additive-only change — all existing `ResourceListPage` consumers pass no `headerContent` and see no difference.

- [ ] **Step 3: Migrate `ServiceList.tsx`**

Rewrite `ServiceList.tsx` to use `ResourceListPage`. The key mapping:

- Move `useMonitoringStatus`, `useServiceMetrics`, `useRecommendations` hooks into the component body (they stay — they feed `headerContent` and conditional columns).
- The `MetricsPanel` block that renders above the toolbar becomes the `headerContent` prop.
- The `baseColumns`, `metricsColumns`, `sizingColumns` merge becomes the `columns` factory.
- The `ServiceStatusBadge` sub-component stays as a local function.
- `renderCard`, `emptyMessage`, `itemPath`, `keyFn` map directly to `ResourceListPage` props.

The component shrinks from ~320 lines to ~180 lines (the column definitions and metrics hooks remain, but loading/error/toolbar/table/card boilerplate is gone).

- [ ] **Step 4: Migrate `NodeList.tsx`**

Same approach. `NodeResourceGauges` + `MetricsPanel` become `headerContent`. Conditional `metricsColumns` move into the `columns` factory. Component shrinks from ~274 lines to ~150 lines.

- [ ] **Step 5: Verify**

Run: `cd frontend && npx vitest run && npx tsc -b --noEmit`

- [ ] **Step 6: Commit**

```
refactor(frontend): migrate ServiceList and NodeList to ResourceListPage

Add headerContent prop to ResourceListPage for metrics panels
rendered between the page header and toolbar.
```

---

### Task 3: Extract `handleCreateDataResource` generic handler (Backend)

**Files:**
- Create: `internal/api/write_create.go`
- Modify: `internal/api/write_config_handlers.go`
- Modify: `internal/api/write_secret_handlers.go`

- [ ] **Step 1: Create `write_create.go` with the generic spec + handler**

Follow the same pattern as `write_remove.go` (`removeSpec` + `handleRemove`):

```go
// internal/api/write_create.go
package api

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// createDataResourceSpec describes how to create a data-bearing resource
// (config or secret).
type createDataResourceSpec struct {
	resource     string // "config" or "secret"
	nameErrCode  string // validation error code (e.g. "CFG004")
	conflictCode string // conflict error code (e.g. "CFG003")
	basePath     string // e.g. "/configs/"
	typeName     string // JSON-LD type: "Config" or "Secret"

	create       func(ctx context.Context, name string, data []byte) (string, error)
	getByID      func(id string) (any, []cache.ServiceRef, bool)
	sanitize     func(resource any) any // optional post-fetch transform (e.g. nil secret data)
}

func handleCreateDataResource(
	w http.ResponseWriter,
	r *http.Request,
	spec createDataResourceSpec,
) {
	req, ok := decodeJSON[createResourceRequest](w, r)
	if !ok {
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeErrorCode(w, r, spec.nameErrCode, "name is required")
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeErrorCode(w, r, spec.nameErrCode, "data must be valid base64")
		return
	}

	slog.Info("creating "+spec.resource, "name", req.Name)

	id, err := spec.create(r.Context(), req.Name, data)
	if err != nil {
		if cerrdefs.IsConflict(err) {
			writeErrorCode(w, r, spec.conflictCode, err.Error())
			return
		}
		writeDockerError(w, r, err, spec.resource, req.Name)
		return
	}

	w.Header().Set("Location", absPath(r.Context(), spec.basePath+id))

	if preferMinimal(r) {
		writePreferCreated(w)
		return
	}

	resource, services, ok := spec.getByID(id)
	if !ok {
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, NewDetailResponse(r.Context(), spec.basePath+id, spec.typeName, resource))
		return
	}

	if spec.sanitize != nil {
		resource = spec.sanitize(resource)
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse(r.Context(), spec.basePath+id, spec.typeName, resource))
}
```

Note: The generic approach above uses `any` for the resource type because `ConfigResponse` and `SecretResponse` are different concrete types. An alternative is to use a type parameter, but since the two callers build their response structs differently (config vs secret field names), `any` with callback functions is simpler here.

Actually, looking more carefully at the code, the cache-miss fallback builds a stub response struct and the cache-hit path builds a full response struct. These response types differ (`ConfigResponse` vs `SecretResponse`). The cleanest approach is to have the spec provide two builder callbacks:

```go
type createDataResourceSpec struct {
	resource     string
	nameErrCode  string
	conflictCode string
	basePath     string
	typeName     string

	create        func(ctx context.Context, name string, data []byte) (string, error)
	buildFallback func(id string, name string) any
	buildResponse func(id string) (any, bool)
}
```

- [ ] **Step 2: Implement the final version of `write_create.go`**

```go
package api

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
)

type createDataResourceSpec struct {
	resource     string // "config" or "secret"
	nameErrCode  string // validation error code
	conflictCode string // create-conflict error code
	basePath     string // URL path prefix including trailing slash
	typeName     string // JSON-LD type name

	create        func(ctx context.Context, name string, data []byte) (string, error)
	buildFallback func(id string, name string) any
	buildResponse func(id string) (any, bool)
}

func handleCreateDataResource(
	w http.ResponseWriter,
	r *http.Request,
	spec createDataResourceSpec,
) {
	req, ok := decodeJSON[createResourceRequest](w, r)
	if !ok {
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeErrorCode(w, r, spec.nameErrCode, "name is required")
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeErrorCode(w, r, spec.nameErrCode, "data must be valid base64")
		return
	}

	slog.Info("creating "+spec.resource, "name", req.Name)

	id, err := spec.create(r.Context(), req.Name, data)
	if err != nil {
		if cerrdefs.IsConflict(err) {
			writeErrorCode(w, r, spec.conflictCode, err.Error())
			return
		}

		writeDockerError(w, r, err, spec.resource, req.Name)
		return
	}

	w.Header().Set("Location", absPath(r.Context(), spec.basePath+id))

	if preferMinimal(r) {
		writePreferCreated(w)
		return
	}

	resp, ok := spec.buildResponse(id)
	if !ok {
		resp = spec.buildFallback(id, req.Name)
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse(r.Context(), spec.basePath+id, spec.typeName, resp))
}
```

- [ ] **Step 3: Rewrite `HandleCreateConfig` in `write_config_handlers.go`**

Replace the existing `HandleCreateConfig` method body:

```go
func (h *Handlers) HandleCreateConfig(w http.ResponseWriter, r *http.Request) {
	handleCreateDataResource(w, r, createDataResourceSpec{
		resource:     "config",
		nameErrCode:  "CFG004",
		conflictCode: "CFG003",
		basePath:     "/configs/",
		typeName:     "Config",
		create: func(ctx context.Context, name string, data []byte) (string, error) {
			return h.configWriter.CreateConfig(ctx, swarm.ConfigSpec{
				Annotations: swarm.Annotations{Name: name},
				Data:        data,
			})
		},
		buildFallback: func(id string, name string) any {
			return ConfigResponse{
				Config:   swarm.Config{ID: id, Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name}}},
				Services: []cache.ServiceRef{},
			}
		},
		buildResponse: func(id string) (any, bool) {
			cfg, ok := h.cache.GetConfig(id)
			if !ok {
				return nil, false
			}
			return ConfigResponse{Config: cfg, Services: h.cache.ServicesUsingConfig(id)}, true
		},
	})
}
```

Remove the now-unused `encoding/base64`, `strings`, and `cerrdefs` imports if no other function in the file uses them. Keep `swarm` and `cache`.

- [ ] **Step 4: Rewrite `HandleCreateSecret` in `write_secret_handlers.go`**

```go
func (h *Handlers) HandleCreateSecret(w http.ResponseWriter, r *http.Request) {
	handleCreateDataResource(w, r, createDataResourceSpec{
		resource:     "secret",
		nameErrCode:  "SEC004",
		conflictCode: "SEC003",
		basePath:     "/secrets/",
		typeName:     "Secret",
		create: func(ctx context.Context, name string, data []byte) (string, error) {
			return h.secretWriter.CreateSecret(ctx, swarm.SecretSpec{
				Annotations: swarm.Annotations{Name: name},
				Data:        data,
			})
		},
		buildFallback: func(id string, name string) any {
			return SecretResponse{
				Secret:   swarm.Secret{ID: id, Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}}},
				Services: []cache.ServiceRef{},
			}
		},
		buildResponse: func(id string) (any, bool) {
			sec, ok := h.cache.GetSecret(id)
			if !ok {
				return nil, false
			}
			sec.Spec.Data = nil
			return SecretResponse{Secret: sec, Services: h.cache.ServicesUsingSecret(id)}, true
		},
	})
}
```

Remove unused imports similarly.

- [ ] **Step 5: Verify**

Run: `go build ./... && go test ./internal/api/...`

- [ ] **Step 6: Commit**

```
refactor(api): extract handleCreateDataResource to deduplicate config/secret creation
```

---

### Task 4: Extract `DataResourceDetail` component for `SecretDetail` / `ConfigDetail`

**Files:**
- Create: `frontend/src/components/DataResourceDetail.tsx`
- Modify: `frontend/src/pages/SecretDetail.tsx`
- Modify: `frontend/src/pages/ConfigDetail.tsx`

- [ ] **Step 1: Create `DataResourceDetail.tsx`**

This component encapsulates the shared skeleton: `useDetailResource` → error/loading guards → `PageHeader` with `RemoveResourceAction` → `MetadataGrid` → `KeyValueEditor` for labels → optional children → `ServiceRefList` → `ActivitySection`.

```tsx
// frontend/src/components/DataResourceDetail.tsx
import type { ServiceRef } from "../api/types";
import ActivitySection from "./ActivitySection";
import { MetadataGrid, ResourceId, ResourceLink, Timestamp } from "./data";
import FetchError from "./FetchError";
import { KeyValueEditor } from "./KeyValueEditor";
import { LoadingDetail } from "./LoadingSkeleton";
import PageHeader from "./PageHeader";
import { RemoveResourceAction } from "./RemoveResourceAction";
import ResourceName from "./ResourceName";
import ServiceRefList from "./ServiceRefList";
import { useDetailResource } from "../hooks/useDetailResource";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
import { parseStackLabels } from "../lib/parseStackLabels";
import type { HistoryEntry } from "../api/types";
import { useEffect, useState, type ReactNode } from "react";
import { useParams } from "react-router-dom";

interface DataResourceDetailProps {
  resourceType: "config" | "secret";
  listPath: string;
  listLabel: string;
  fetchFn: (id: string) => Promise<any>;
  ssePath: (id: string) => string;
  extractResource: (data: any) => { id: string; name: string; labels: Record<string, string>; createdAt: string; updatedAt: string };
  extractServices: (data: any) => ServiceRef[];
  patchLabels: (id: string, ops: any) => Promise<Record<string, string>>;
  removeResource: (id: string) => Promise<void>;
  children?: (data: any) => ReactNode;
}
```

Actually, this is getting over-abstracted. The two pages are 125 and 153 lines — the shared component would need so many props that it wouldn't save much and would be harder to read. A simpler approach: extract just the repeated sub-patterns into small helpers, or accept a modest amount of duplication for clarity.

Let me reconsider. The real duplication is the page structure — both pages have the same 8-section layout with the same hooks and guards. The config page just adds one extra section (data viewer). The simplest extraction that doesn't over-abstract is a component that takes the resource + services + history + allowedMethods as already-fetched data, plus a few config strings:

```tsx
// frontend/src/components/DataResourceDetail.tsx
import type { ServiceRef } from "../api/types";
import type { HistoryEntry } from "../api/types";
import ActivitySection from "./ActivitySection";
import { MetadataGrid, ResourceId, ResourceLink, Timestamp } from "./data";
import { KeyValueEditor } from "./KeyValueEditor";
import { RemoveResourceAction } from "./RemoveResourceAction";
import ResourceName from "./ResourceName";
import ServiceRefList from "./ServiceRefList";
import PageHeader from "./PageHeader";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
import { parseStackLabels } from "../lib/parseStackLabels";
import { useEffect, useState, type ReactNode } from "react";

interface DataResourceDetailProps {
  resourceType: "config" | "secret";
  listLabel: string;
  listPath: string;
  id: string;
  name: string;
  labels: Record<string, string>;
  createdAt: string;
  updatedAt: string;
  services: ServiceRef[];
  history: HistoryEntry[];
  allowedMethods: Set<string>;
  onRemove: () => Promise<void>;
  onPatchLabels: (ops: any) => Promise<Record<string, string>>;
  children?: ReactNode;
}

export default function DataResourceDetail({
  resourceType,
  listLabel,
  listPath,
  id,
  name,
  labels: initialLabels,
  createdAt,
  updatedAt,
  services,
  history,
  allowedMethods,
  onRemove,
  onPatchLabels,
  children,
}: DataResourceDetailProps) {
  const [labels, setLabels] = useState<Record<string, string>>(initialLabels);
  const { stack } = parseStackLabels(initialLabels);

  useEffect(() => {
    setLabels(initialLabels);
  }, [initialLabels]);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={<ResourceName name={name} direction="column" />}
        breadcrumbs={[
          { label: listLabel, to: listPath },
          { label: <ResourceName name={name} /> },
        ]}
        actions={
          <RemoveResourceAction
            resourceType={resourceType}
            resourceName={name}
            listPath={stack ? `/stacks/${stack}` : listPath}
            onRemove={onRemove}
            canDelete={allowedMethods.has("DELETE")}
            disabled={services.length > 0}
            disabledTitle={`Cannot remove a ${resourceType} that is in use by services`}
          />
        }
      />

      <MetadataGrid>
        <ResourceId label="ID" id={id} />
        <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
        <Timestamp label="Created" date={createdAt} />
        <Timestamp label="Updated" date={updatedAt} />
      </MetadataGrid>

      <KeyValueEditor
        title="Labels"
        entries={labels}
        defaultOpen={Object.keys(labels).length > 0}
        keyPlaceholder="com.example.my-label"
        valuePlaceholder="value"
        editDisabled={!allowedMethods.has("PATCH")}
        isKeyReadOnly={isReservedLabelKey}
        validateKey={validateLabelKey}
        onSave={async (ops) => {
          const updated = await onPatchLabels(ops);
          setLabels(updated);
          return updated;
        }}
      />

      {children}

      <ServiceRefList
        services={services}
        label="Used by Services"
        emptyMessage={`No services using this ${resourceType}.`}
      />

      <ActivitySection entries={history} hideType />
    </div>
  );
}
```

- [ ] **Step 2: Rewrite `SecretDetail.tsx`**

```tsx
import { api } from "../api/client";
import DataResourceDetail from "../components/DataResourceDetail";
import FetchError from "../components/FetchError";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { useDetailResource } from "../hooks/useDetailResource";
import { useParams } from "react-router-dom";

export default function SecretDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error, retry, allowedMethods } = useDetailResource(
    id,
    api.secret,
    `/secrets/${id}`,
  );

  if (error) {
    return <FetchError message={error.message || "Failed to load secret"} onRetry={retry} />;
  }

  if (!data) {
    return <LoadingDetail />;
  }

  const { secret } = data;
  const services = data.services ?? [];
  const name = secret.Spec.Name || secret.ID;

  return (
    <DataResourceDetail
      resourceType="secret"
      listLabel="Secrets"
      listPath="/secrets"
      id={secret.ID}
      name={name}
      labels={secret.Spec.Labels ?? {}}
      createdAt={secret.CreatedAt}
      updatedAt={secret.UpdatedAt}
      services={services}
      history={history}
      allowedMethods={allowedMethods}
      onRemove={() => api.removeSecret(secret.ID)}
      onPatchLabels={(ops) => api.patchSecretLabels(secret.ID, ops)}
    />
  );
}
```

Down from 125 to ~35 lines.

- [ ] **Step 3: Rewrite `ConfigDetail.tsx`**

```tsx
import { api } from "../api/client";
import CodeBlock from "../components/CodeBlock";
import CollapsibleSection from "../components/CollapsibleSection";
import DataResourceDetail from "../components/DataResourceDetail";
import FetchError from "../components/FetchError";
import { IconButton } from "../components/IconButton";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { useDetailResource } from "../hooks/useDetailResource";
import { Copy } from "lucide-react";
import { useParams } from "react-router-dom";

export default function ConfigDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, history, error, retry, allowedMethods } = useDetailResource(
    id,
    api.config,
    `/configs/${id}`,
  );

  if (error) {
    return <FetchError message={error.message || "Failed to load config"} onRetry={retry} />;
  }

  if (!data) {
    return <LoadingDetail />;
  }

  const { config } = data;
  const services = data.services ?? [];
  const name = config.Spec.Name || config.ID;

  let decoded: string | null = null;
  if (config.Spec.Data) {
    try {
      decoded = atob(config.Spec.Data);
    } catch {
      decoded = null;
    }
  }

  return (
    <DataResourceDetail
      resourceType="config"
      listLabel="Configs"
      listPath="/configs"
      id={config.ID}
      name={name}
      labels={config.Spec.Labels ?? {}}
      createdAt={config.CreatedAt}
      updatedAt={config.UpdatedAt}
      services={services}
      history={history}
      allowedMethods={allowedMethods}
      onRemove={() => api.removeConfig(config.ID)}
      onPatchLabels={(ops) => api.patchConfigLabels(config.ID, ops)}
    >
      {decoded != null && (
        <CollapsibleSection
          title="Data"
          controls={
            <IconButton
              onClick={() => navigator.clipboard.writeText(decoded)}
              title="Copy"
              icon={<Copy className="size-3.5" />}
            />
          }
        >
          <CodeBlock code={decoded} />
        </CollapsibleSection>
      )}
    </DataResourceDetail>
  );
}
```

Down from 153 to ~55 lines.

- [ ] **Step 4: Verify**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`

- [ ] **Step 5: Commit**

```
refactor(frontend): extract DataResourceDetail to deduplicate SecretDetail/ConfigDetail
```

---

### Task 5: Final verification

- [ ] **Step 1: Run full backend tests**

Run: `go test ./...`

- [ ] **Step 2: Run full frontend suite**

Run: `cd frontend && npx vitest run && npm run lint && npx tsc -b --noEmit`

- [ ] **Step 3: Fix any failures**

All changes are mechanical. Failures indicate a mistake in the replacement.
