# SSE-Derived Sub-Resource State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate redundant HTTP requests on SSE events by deriving sub-resource state from the SSE event payload.

**Architecture:** Replace the monolithic `fetchData` in ServiceDetail and NodeDetail with a split: `fetchService`/`fetchNode` (initial load + sync fallback) and `handleSSEEvent` (uses the event's `resource` field to update state without HTTP calls). Only `tasks` and `history` still need HTTP refetches.

**Tech Stack:** React 19, TypeScript

**Spec:** `docs/superpowers/specs/2026-03-23-sse-derived-sub-resources-design.md`

---

### Task 1: Create `deriveServiceSubResources` Helper

**Files:**
- Create: `frontend/src/lib/deriveServiceState.ts`

This is a pure function module — no React, no hooks. It takes a `Service` object and returns all derived sub-resource values. Extracting this to a separate file keeps ServiceDetail.tsx focused on orchestration.

- [ ] **Step 1: Create the helper module**

Create `frontend/src/lib/deriveServiceState.ts`:

```ts
import type {
  ContainerConfig,
  Healthcheck,
  PortConfig,
  ServiceConfigRef,
  ServiceMount,
  ServiceNetworkRef,
  ServiceSecretRef,
  Service,
} from "@/api/types";
import type { ServiceResourceShape } from "@/components/service-detail";

export interface DerivedServiceState {
  envVars: Record<string, string>;
  serviceResources: ServiceResourceShape | null;
  serviceLabels: Record<string, string>;
  healthcheck: Healthcheck | null;
  specPorts: PortConfig[];
  serviceMounts: ServiceMount[];
  containerConfig: ContainerConfig;
  serviceConfigs: ServiceConfigRef[];
  serviceSecrets: ServiceSecretRef[];
  serviceNetworks: ServiceNetworkRef[];
}

/**
 * Derives all sub-resource state from a Service object.
 * Replicates the server-side transformations that the sub-resource
 * GET endpoints perform (envSliceToMap, extractConfigRefs, etc.).
 */
export function deriveServiceSubResources(service: Service): DerivedServiceState {
  const spec = service.Spec;
  const taskTemplate = spec.TaskTemplate;
  const containerSpec = taskTemplate.ContainerSpec;

  return {
    envVars: envSliceToMap(containerSpec.Env),
    serviceResources: taskTemplate.Resources ?? null,
    serviceLabels: spec.Labels ?? {},
    healthcheck: containerSpec.Healthcheck ?? null,
    specPorts: spec.EndpointSpec?.Ports ?? [],
    serviceMounts: containerSpec.Mounts ?? [],
    containerConfig: containerConfigFromSpec(containerSpec),
    serviceConfigs: extractConfigRefs(containerSpec.Configs),
    serviceSecrets: extractSecretRefs(containerSpec.Secrets),
    serviceNetworks: extractNetworkRefs(taskTemplate.Networks),
  };
}

function envSliceToMap(env?: string[]): Record<string, string> {
  const result: Record<string, string> = {};

  if (!env) {
    return result;
  }

  for (const entry of env) {
    const index = entry.indexOf("=");

    if (index >= 0) {
      result[entry.substring(0, index)] = entry.substring(index + 1);
    } else {
      result[entry] = "";
    }
  }

  return result;
}

function containerConfigFromSpec(spec: Service["Spec"]["TaskTemplate"]["ContainerSpec"]): ContainerConfig {
  return {
    command: spec.Command,
    args: spec.Args,
    dir: spec.Dir ?? "",
    user: spec.User ?? "",
    hostname: spec.Hostname ?? "",
    init: spec.Init,
    tty: spec.TTY ?? false,
    readOnly: spec.ReadOnly ?? false,
    stopSignal: spec.StopSignal ?? "",
    stopGracePeriod: spec.StopGracePeriod,
    capabilityAdd: spec.CapabilityAdd,
    capabilityDrop: spec.CapabilityDrop,
    groups: spec.Groups,
    hosts: spec.Hosts,
    dnsConfig: spec.DNSConfig
      ? {
          nameservers: spec.DNSConfig.Nameservers,
          search: spec.DNSConfig.Search,
          options: spec.DNSConfig.Options,
        }
      : undefined,
  };
}

function extractConfigRefs(
  configs?: Array<{ ConfigID: string; ConfigName: string; File?: { Name: string } }>,
): ServiceConfigRef[] {
  if (!configs) {
    return [];
  }

  return configs.map(({ ConfigID, ConfigName, File }) => ({
    configID: ConfigID,
    configName: ConfigName,
    fileName: File?.Name ?? "",
  }));
}

function extractSecretRefs(
  secrets?: Array<{ SecretID: string; SecretName: string; File?: { Name: string } }>,
): ServiceSecretRef[] {
  if (!secrets) {
    return [];
  }

  return secrets.map(({ SecretID, SecretName, File }) => ({
    secretID: SecretID,
    secretName: SecretName,
    fileName: File?.Name ?? "",
  }));
}

function extractNetworkRefs(
  networks?: Array<{ Target: string; Aliases?: string[] }>,
): ServiceNetworkRef[] {
  if (!networks) {
    return [];
  }

  return networks.map(({ Target, Aliases }) => ({
    target: Target,
    aliases: Aliases,
  }));
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/lib/deriveServiceState.ts
git commit -m "feat: add deriveServiceSubResources helper for SSE-based state updates"
```

---

### Task 2: Refactor ServiceDetail to Use SSE Payload

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

This is the core refactoring. Replace the monolithic `fetchData` with split functions and an SSE listener that uses the event payload.

- [ ] **Step 1: Add import**

Add at the top of ServiceDetail.tsx:

```ts
import { deriveServiceSubResources } from "../lib/deriveServiceState";
```

Also add `ServiceConfigRef`, `ServiceSecretRef`, `ServiceNetworkRef` to the type imports from `../api/types` (these are needed for the new state variables that replace inline derivation).

- [ ] **Step 2: Replace `fetchData` with split functions**

Replace the entire `fetchData` useCallback (lines 84-141) and the `useResourceStream` call (line 162) with:

```ts
const fetchService = useCallback(
  (signal: AbortSignal) => {
    api
      .service(id!, signal)
      .then((response) => {
        setService(response.service);
        setChanges(response.changes ?? []);
        applyDerivedState(response.service);
      })
      .catch(() => {
        if (!signal.aborted) {
          setError(true);
        }
      });
  },
  [id],
);

const fetchSideData = useCallback(
  (signal: AbortSignal) => {
    api
      .serviceTasks(id!, signal)
      .then(setTasks)
      .catch(() => {});
    api
      .history({ resourceId: id!, limit: 10 }, signal)
      .then(setHistory)
      .catch(() => {});
  },
  [id],
);

function applyDerivedState(service: Service) {
  const derived = deriveServiceSubResources(service);
  setEnvVars(derived.envVars);
  setServiceResources(derived.serviceResources);
  setServiceLabels(derived.serviceLabels);
  setHealthcheck(derived.healthcheck);
  setSpecPorts(derived.specPorts);
  setServiceMounts(derived.serviceMounts);
  setContainerConfig(derived.containerConfig);
}
```

Note: `applyDerivedState` does NOT set configs/secrets/networks state because those are currently derived inline in the JSX (not stored as state). If the page has separate state for those, set them here too. Check the current file for `serviceConfigs`/`serviceSecrets` state variables. If they exist, add them to `applyDerivedState`. If not, the inline derivation from `service.Spec` is sufficient.

- [ ] **Step 3: Replace the mount and side-effect hooks**

Replace the `useEffect` that calls `fetchData()` (lines 157-160) and the `useResourceStream` call (line 162) with:

```ts
useEffect(() => {
  if (!id) {
    return;
  }

  abortRef.current?.abort();
  const controller = new AbortController();
  abortRef.current = controller;

  fetchService(controller.signal);
  fetchSideData(controller.signal);

  return () => controller.abort();
}, [id, fetchService, fetchSideData]);

useResourceStream(`/services/${id}`, (event) => {
  if (!id) {
    return;
  }

  if (event.resource) {
    const service = event.resource as Service;
    setService(service);
    applyDerivedState(service);
  }

  // Always refetch tasks and history — they're not in the service object
  const controller = new AbortController();
  abortRef.current?.abort();
  abortRef.current = controller;
  fetchSideData(controller.signal);

  // On sync events (no resource), also refetch service for fresh changes/diff
  if (!event.resource) {
    fetchService(controller.signal);
  }
});
```

- [ ] **Step 4: Remove the old sub-resource API calls**

Delete the individual `api.serviceEnv`, `api.serviceResources`, `api.serviceLabels`, `api.serviceHealthcheck`, `api.servicePorts`, `api.serviceMounts`, `api.serviceContainerConfig` calls that were in the old `fetchData`. These are now handled by `applyDerivedState`.

- [ ] **Step 5: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 6: Verify lint passes**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`

- [ ] **Step 7: Fix formatting**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run fmt`

- [ ] **Step 8: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "refactor: derive service sub-resources from SSE payload instead of refetching"
```

---

### Task 3: Refactor NodeDetail to Use SSE Payload

**Files:**
- Modify: `frontend/src/pages/NodeDetail.tsx`

Same pattern as ServiceDetail but simpler — only `nodeLabels` is derivable.

- [ ] **Step 1: Replace `fetchData` with split functions**

Replace the entire `fetchData` useCallback (lines 52-86) and the hooks that use it (lines 88-93) with:

```ts
const fetchNode = useCallback(
  (signal: AbortSignal) => {
    api
      .node(id!, signal)
      .then((node) => {
        setNode(node);
        setNodeLabels(node.Spec.Labels ?? {});
      })
      .catch(() => {
        if (!signal.aborted) {
          setError(true);
        }
      });
  },
  [id],
);

const fetchSideData = useCallback(
  (signal: AbortSignal) => {
    api
      .nodeTasks(id!, signal)
      .then(setTasks)
      .catch(() => {});
    api
      .history({ resourceId: id!, limit: 10 }, signal)
      .then(setHistory)
      .catch(() => {});
    api
      .nodeRole(id!, signal)
      .then(({ managerCount: count }) => setManagerCount(count))
      .catch(() => {});
  },
  [id],
);

useEffect(() => {
  if (!id) {
    return;
  }

  abortRef.current?.abort();
  const controller = new AbortController();
  abortRef.current = controller;

  fetchNode(controller.signal);
  fetchSideData(controller.signal);

  return () => controller.abort();
}, [id, fetchNode, fetchSideData]);

useResourceStream(`/nodes/${id}`, (event) => {
  if (!id) {
    return;
  }

  if (event.resource) {
    const node = event.resource as Node;
    setNode(node);
    setNodeLabels(node.Spec.Labels ?? {});
  }

  const controller = new AbortController();
  abortRef.current?.abort();
  abortRef.current = controller;
  fetchSideData(controller.signal);

  if (!event.resource) {
    fetchNode(controller.signal);
  }
});
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 3: Fix formatting**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run fmt`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/NodeDetail.tsx
git commit -m "refactor: derive node labels from SSE payload instead of refetching"
```

---

### Task 4: Full Verification

**Files:** None (verification only)

- [ ] **Step 1: Run frontend type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`

- [ ] **Step 2: Run frontend lint and format check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint && npm run fmt:check`

- [ ] **Step 3: Run all backend tests**

Run: `cd /Users/moritz/GolandProjects/cetacean && go test ./... -count=1`

- [ ] **Step 4: Run full make check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make check`
Expected: All checks pass
