# Browser Demo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Embed a fully interactive Cetacean demo on the website that runs entirely in the browser — no backend required. Visitors see a working dashboard with fake swarm data, live events, and updating metrics.

**Architecture:** The Cetacean SPA is embedded in a full-page Astro route (`/demo`). MSW (Mock Service Worker) intercepts all API calls and serves responses from an in-memory dataset. An event simulator runs on `setInterval`, mutating the dataset and pushing events via MSW's `sse()` API. The SPA code is unchanged — it calls `fetch("/nodes")` and opens `EventSource("/events")` as normal.

**Tech Stack:** Astro (existing website), MSW 2.x (with SSE support), TypeScript, the existing React SPA (embedded via `<iframe>` or built as a standalone page)

---

## Key Design Decision: Iframe vs. Integrated Build

**Iframe approach** (recommended): The website's `/demo` page contains a full-viewport `<iframe>` pointing to a separately-built version of the SPA that bundles MSW + demo data. The SPA is built with a Vite config variant that includes the MSW worker and demo module.

**Why iframe:** The SPA has its own router, CSS reset, and global state (auth context, SSE connections). Embedding React components directly into Astro would require re-wiring all of that. An iframe is a clean boundary — the SPA runs exactly as it would against a real backend, just with MSW intercepting everything.

**Deployment:** The demo SPA build outputs to `website/public/demo/` so Astro serves it as a static asset. The MSW service worker (`mockServiceWorker.js`) lives in the same directory.

---

## File Structure

```
frontend/
  src/
    demo/
      dataset.ts          — TypeScript port of the Go dataset (nodes, services, tasks, etc.)
      handlers.ts          — MSW http handlers for all API endpoints
      sseHandlers.ts       — MSW sse handlers for EventSource endpoints
      simulator.ts         — Event simulator (setInterval-based state mutations)
      prometheus.ts        — Fake Prometheus query/range responses
      timeseries.ts        — Time-series data generator (port from Go)
      worker.ts            — MSW worker setup (setupWorker + handlers)
      index.ts             — Entry point: start MSW, then mount React app
  vite.config.demo.ts      — Vite config for demo build (sets DEMO env var, output to website)
website/
  src/
    pages/
      demo.astro           — Full-viewport iframe page
  public/
    demo/                  — Output of demo SPA build (index.html, assets, mockServiceWorker.js)
```

## Conventions

- The demo module tree-shakes out of the normal SPA build via `import.meta.env.VITE_DEMO` guard
- MSW's `setupWorker()` is browser-only — the demo module is never imported in the normal build
- All demo data uses the same IDs/names/structure as the Go demo server for consistency
- The SSE event format matches what `useResourceStream` expects: `{ type, action, id, resource? }`
- Prometheus mock returns the same per-service profiles as the Go version

---

### Task 1: MSW Setup and Basic HTTP Handlers

**Files:**
- Create: `frontend/src/demo/dataset.ts`
- Create: `frontend/src/demo/handlers.ts`
- Create: `frontend/src/demo/worker.ts`
- Modify: `frontend/package.json` (add `msw` dependency)

- [ ] **Step 1: Install MSW**

```bash
cd frontend && npm install msw --save-dev
```

- [ ] **Step 2: Initialize MSW service worker**

```bash
cd frontend && npx msw init ../website/public/demo --save
```

This creates `website/public/demo/mockServiceWorker.js`.

- [ ] **Step 3: Create the dataset**

Port the Go `dataset.go` to TypeScript. This is the same data — 3 nodes, 11 services, 23 tasks, 3 configs, 2 secrets, 5 networks, 2 volumes — just expressed as TypeScript objects matching the Docker API JSON types already defined in `frontend/src/api/types.ts`.

```typescript
// frontend/src/demo/dataset.ts
import type { Node, Service, Task, Config, Secret, Network, Volume } from "@/api/types";

export interface Dataset {
  nodes: Node[];
  services: Service[];
  tasks: Task[];
  configs: Config[];
  secrets: Secret[];
  networks: Network[];
  volumes: Volume[];
  swarm: Swarm;
}

export function buildDataset(): Dataset { ... }
```

Use the same IDs, names, labels, and cross-references as the Go version. The types from `@/api/types.ts` should match the Docker JSON structures since that's what the Go backend returns.

Check `frontend/src/api/types.ts` for the exact type definitions — use those rather than inventing new ones.

- [ ] **Step 4: Create MSW HTTP handlers**

```typescript
// frontend/src/demo/handlers.ts
import { http, HttpResponse } from "msw";
import type { Dataset } from "./dataset";

export function createHandlers(dataset: Dataset) {
  return [
    // List endpoints — return CollectionResponse wrappers (matching Go's JSON-LD format)
    http.get("*/nodes", () => {
      return HttpResponse.json(collectionResponse(dataset.nodes, "nodes"));
    }),
    http.get("*/services", () => { ... }),
    // ... all list endpoints

    // Detail endpoints — return DetailResponse wrappers
    http.get("*/nodes/:id", ({ params }) => {
      const node = dataset.nodes.find(n => n.ID === params.id);
      if (!node) return new HttpResponse(null, { status: 404 });
      return HttpResponse.json(detailResponse(node, "Node", params.id));
    }),
    // ... all detail endpoints

    // System endpoints
    http.get("*/-/health", () => HttpResponse.json({ status: "ok" })),
    http.get("*/-/ready", () => HttpResponse.json({ status: "ready" })),
    http.get("*/cluster", () => { ... }),
    http.get("*/cluster/metrics", () => { ... }),
    http.get("*/swarm", () => { ... }),
    http.get("*/search", ({ request }) => { ... }),
    http.get("*/stacks/summary", () => { ... }),
    http.get("*/history", () => { ... }),

    // Auth
    http.get("*/auth/whoami", () => HttpResponse.json({ mode: "none" })),

    // Metrics — delegate to prometheus mock
    http.get("*/metrics", ({ request }) => { ... }),
    http.get("*/metrics/status", () => { ... }),
    http.get("*/metrics/labels", () => { ... }),
    http.get("*/metrics/labels/:name", () => { ... }),

    // Recommendations
    http.get("*/recommendations", () => { ... }),
  ];
}
```

Important: The handlers must return responses in the **Cetacean API format** (JSON-LD wrappers with `@context`, `@id`, `@type`, `items`, `total`, pagination), NOT raw Docker API format. The Go demo server speaks Docker API; these MSW handlers replace the *Cetacean backend*, not the Docker daemon.

Study the response shapes by reading the existing test fixtures in `frontend/src/` or by examining what `useSwarmResource` expects.

- [ ] **Step 5: Create worker setup**

```typescript
// frontend/src/demo/worker.ts
import { setupWorker } from "msw/browser";
import { buildDataset } from "./dataset";
import { createHandlers } from "./handlers";

const dataset = buildDataset();
export const worker = setupWorker(...createHandlers(dataset));
```

- [ ] **Step 6: Verify it compiles**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 7: Commit**

```bash
git add frontend/src/demo/ frontend/package.json frontend/package-lock.json
git commit -m "feat(demo): add MSW dataset and HTTP handlers for browser demo"
```

---

### Task 2: SSE Handlers

**Files:**
- Create: `frontend/src/demo/sseHandlers.ts`
- Modify: `frontend/src/demo/worker.ts`

- [ ] **Step 1: Create SSE handlers**

MSW 2.12+ has `sse()` API for intercepting `EventSource`:

```typescript
// frontend/src/demo/sseHandlers.ts
import { sse } from "msw";
import type { Dataset } from "./dataset";

// The Cetacean SSE format: named events with JSON data.
// Event names: "node", "service", "task", etc.
// Data: { type, action, id, resource? }

export function createSSEHandlers(dataset: Dataset) {
  // Store client references so the simulator can push events later.
  const clients: Set<SSEClient> = new Set();

  const handlers = [
    // Global events endpoint
    sse("*/events", ({ client }) => {
      clients.add(client);
      client.addEventListener("close", () => clients.delete(client));
    }),

    // Per-resource SSE endpoints (used by detail pages)
    sse("*/nodes", ({ client }) => { ... }),
    sse("*/nodes/:id", ({ client }) => { ... }),
    sse("*/services", ({ client }) => { ... }),
    sse("*/services/:id", ({ client }) => { ... }),
    // ... all resource types
  ];

  return { handlers, clients };
}
```

Check the exact MSW SSE API at https://mswjs.io/docs/sse/ — the `client.send()` method and event naming may differ from the sketch above. The `sse()` handler receives `{ client, request, params }`.

The Cetacean SSE format uses named events (not the default `message` event). Each event has:
- Event name: the resource type (`node`, `service`, `task`, etc.) or `sync` or `batch`
- Data: JSON `{ type: string, action: string, id: string, resource?: object }`

Check `frontend/src/hooks/useResourceStream.ts` for the exact event names and data format the SPA expects.

- [ ] **Step 2: Wire SSE handlers into worker**

```typescript
// frontend/src/demo/worker.ts
import { setupWorker } from "msw/browser";
import { buildDataset } from "./dataset";
import { createHandlers } from "./handlers";
import { createSSEHandlers } from "./sseHandlers";

const dataset = buildDataset();
const { handlers: sseHandlers, clients } = createSSEHandlers(dataset);

export const worker = setupWorker(
  ...createHandlers(dataset),
  ...sseHandlers,
);

// Export clients set so the simulator can push events.
export { clients, dataset };
```

- [ ] **Step 3: Verify it compiles**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/demo/sseHandlers.ts frontend/src/demo/worker.ts
git commit -m "feat(demo): add MSW SSE handlers for EventSource interception"
```

---

### Task 3: Event Simulator

**Files:**
- Create: `frontend/src/demo/simulator.ts`
- Modify: `frontend/src/demo/worker.ts`

- [ ] **Step 1: Create the simulator**

Port the Go simulator logic to TypeScript. Uses `setInterval` instead of goroutines.

```typescript
// frontend/src/demo/simulator.ts
import type { Dataset } from "./dataset";

interface SSEClient {
  send(event: { event: string; data: string }): void;
}

export function startSimulator(dataset: Dataset, clients: Set<SSEClient>) {
  // Run every 5-15 seconds (random).
  function scheduleNext() {
    const delay = 5000 + Math.random() * 10000;
    setTimeout(() => {
      runScenario();
      scheduleNext();
    }, delay);
  }

  function runScenario() {
    const roll = Math.random() * 100;
    if (roll < 40) taskRestart();
    else if (roll < 55) serviceScale();
    else if (roll < 75) rollingUpdate();
    else if (roll < 90) taskFail();
    else nodePressure();
  }

  function broadcast(type: string, action: string, id: string, resource?: unknown) {
    const data = JSON.stringify({ type, action, id, resource });
    for (const client of clients) {
      client.send({ event: type, data });
    }
  }

  function taskRestart() { ... }
  function serviceScale() { ... }
  function rollingUpdate() { ... }
  function taskFail() { ... }
  function nodePressure() { ... }

  scheduleNext();
}
```

The simulator mutates `dataset` in place (same objects the handlers read from) and broadcasts events to all SSE clients. Since JavaScript is single-threaded, no mutex is needed.

- [ ] **Step 2: Start simulator after MSW is ready**

```typescript
// In the demo entry point, after worker.start():
import { startSimulator } from "./simulator";
startSimulator(dataset, clients);
```

- [ ] **Step 3: Verify it compiles**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add frontend/src/demo/simulator.ts frontend/src/demo/worker.ts
git commit -m "feat(demo): add event simulator with task churn, scaling, and rolling updates"
```

---

### Task 4: Prometheus Mock

**Files:**
- Create: `frontend/src/demo/prometheus.ts`
- Create: `frontend/src/demo/timeseries.ts`
- Modify: `frontend/src/demo/handlers.ts`

- [ ] **Step 1: Create time-series generator**

Port `timeseries.go` to TypeScript:

```typescript
// frontend/src/demo/timeseries.ts

export interface ServiceProfile {
  cpuBase: number;
  cpuSpike: number;
  memBase: number;
  memDrift: number;
  period: number;
  noiseScale: number;
}

export const serviceProfiles: Record<string, ServiceProfile> = {
  "webshop_frontend": { cpuBase: 8, cpuSpike: 6, memBase: 90*MB, ... },
  // ... same profiles as Go version
};

export function generateTimeSeries(
  start: number, end: number, step: number,
  base: number, amplitude: number, noise: number, period: number,
): [number, string][] { ... }

export function generateInstantValue(base: number, jitter: number): string { ... }
```

- [ ] **Step 2: Create Prometheus response handlers**

```typescript
// frontend/src/demo/prometheus.ts
// Handles /metrics, /metrics/status, /metrics/labels, /metrics/labels/:name
// Pattern-matches on PromQL query string, returns per-service results
```

These are added to the MSW handlers array — they intercept `GET */metrics?query=...` and return Prometheus-formatted JSON wrapped in the Cetacean proxy response format.

Important: Check how the Cetacean backend proxies Prometheus responses. The frontend sends requests to `/metrics` on the Cetacean server, which proxies to Prometheus. The MSW handler needs to return the response in whatever format the proxy passes through — likely the raw Prometheus API envelope `{ status: "success", data: { resultType, result } }`.

Also handle `/metrics/status` which returns `{ prometheusConfigured, prometheusReachable, nodeExporter: { targets, nodes }, cadvisor: { targets, nodes } }`.

- [ ] **Step 3: Wire into handlers**

Add the Prometheus handlers to the handlers array in `handlers.ts`.

- [ ] **Step 4: Verify it compiles**

```bash
cd frontend && npx tsc -b --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/demo/prometheus.ts frontend/src/demo/timeseries.ts frontend/src/demo/handlers.ts
git commit -m "feat(demo): add Prometheus mock with per-service metric profiles"
```

---

### Task 5: Demo Entry Point and Vite Config

**Files:**
- Create: `frontend/src/demo/index.ts`
- Create: `frontend/vite.config.demo.ts`
- Modify: `frontend/package.json` (add `build:demo` script)

- [ ] **Step 1: Create demo entry point**

```typescript
// frontend/src/demo/index.ts
// This replaces main.tsx as the entry point for the demo build.

import { worker, dataset, clients } from "./worker";
import { startSimulator } from "./simulator";

async function startDemo() {
  await worker.start({
    // Don't warn about unhandled requests — some assets won't be mocked.
    onUnhandledRequest: "bypass",
    // Service worker location relative to the demo build output.
    serviceWorker: {
      url: "./mockServiceWorker.js",
    },
  });

  startSimulator(dataset, clients);

  // Now mount the React app — dynamic import so MSW is ready first.
  const { default: mount } = await import("../main");
  // Or if main.tsx doesn't export a mount function, just importing it
  // triggers ReactDOM.createRoot().render().
}

startDemo();
```

Check how `frontend/src/main.tsx` works — if it auto-mounts on import, the dynamic import approach works. If it needs explicit mounting, adjust accordingly.

- [ ] **Step 2: Create demo Vite config**

```typescript
// frontend/vite.config.demo.ts
import { defineConfig, mergeConfig } from "vite";
import baseConfig from "./vite.config";

export default mergeConfig(
  baseConfig,
  defineConfig({
    // Override entry point to demo/index.ts.
    build: {
      outDir: "../website/public/demo",
      emptyOutDir: true,
      rollupOptions: {
        input: "index.html", // same index.html, but entry script changes
      },
    },
    define: {
      "import.meta.env.VITE_DEMO": "true",
    },
  }),
);
```

The entry point swap needs thought. Options:
- A separate `demo.html` that imports `src/demo/index.ts` instead of `src/main.tsx`
- Or modify `main.tsx` to conditionally import the demo module when `VITE_DEMO` is set

The simpler approach: create `frontend/demo.html` as a copy of `index.html` but with `<script src="src/demo/index.ts">` instead of `<script src="src/main.tsx">`. Use this as the Vite input for the demo build.

- [ ] **Step 3: Add build script**

```json
{
  "scripts": {
    "build:demo": "vite build --config vite.config.demo.ts"
  }
}
```

- [ ] **Step 4: Test the demo build**

```bash
cd frontend && npm run build:demo
ls ../website/public/demo/
```

Expected: `index.html`, `assets/`, `mockServiceWorker.js` all present.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/demo/index.ts frontend/vite.config.demo.ts frontend/demo.html frontend/package.json
git commit -m "feat(demo): add demo entry point and Vite config for browser build"
```

---

### Task 6: Website Demo Page

**Files:**
- Create: `website/src/pages/demo.astro`

- [ ] **Step 1: Create the demo page**

```astro
---
// website/src/pages/demo.astro
// Full-viewport iframe embedding the demo SPA.
---

<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Cetacean Demo</title>
    <style>
      * { margin: 0; padding: 0; }
      html, body { height: 100%; overflow: hidden; }
      iframe {
        width: 100%;
        height: 100%;
        border: none;
      }
    </style>
  </head>
  <body>
    <iframe src="/demo/index.html" title="Cetacean Demo"></iframe>
  </body>
</html>
```

- [ ] **Step 2: Add a "Try Demo" link to the website navigation/hero**

Add a prominent link from the website's index page or nav to `/demo`.

- [ ] **Step 3: Test locally**

```bash
cd frontend && npm run build:demo
cd ../website && npm run dev
# Open http://localhost:4321/demo
```

Expected: The Cetacean dashboard loads inside the iframe, populated with demo data. The MSW service worker intercepts all API calls. Events flow, charts render.

- [ ] **Step 4: Commit**

```bash
git add website/src/pages/demo.astro
git commit -m "feat(website): add /demo page with embedded interactive dashboard"
```

---

### Task 7: Smoke Test and Polish

- [ ] **Step 1: Verify all dashboard pages work**

Navigate through in the browser:
1. Nodes page — 3 nodes visible
2. Services page — 11 services across 3 stacks
3. Service detail — click a service, see tasks, cross-references
4. Stacks page — webshop, monitoring, infra
5. Configs/Secrets — present with stack labels
6. Networks/Volumes — populated
7. Metrics — charts render with per-service profiles
8. Activity feed — events appear as simulator runs
9. Search (Cmd+K) — finds resources
10. Recommendations — missing healthcheck, single replica warnings appear

- [ ] **Step 2: Fix any issues found**

- [ ] **Step 3: Add to CI/build pipeline**

Add the demo build to the website build step so it's deployed alongside the docs:

```bash
# In the website build script or CI:
cd frontend && npm run build:demo
cd website && npm run build
```

- [ ] **Step 4: Commit fixes**

```bash
git add -A
git commit -m "fix(demo): address issues found during browser demo smoke test"
```

---

## Response Format Reference

The MSW handlers need to return responses matching the Cetacean API format, not raw Docker API. Key formats:

**Collection (list endpoints):**
```json
{
  "@context": "/api/context.jsonld",
  "@type": "Collection",
  "items": [...],
  "total": 11,
  "limit": 50,
  "offset": 0
}
```

**Detail (single resource):**
```json
{
  "@context": "/api/context.jsonld",
  "@id": "/services/sv1aaa...",
  "@type": "Service",
  ...resourceFields,
  "services": [...]  // cross-references (for configs, secrets, etc.)
}
```

**SSE events (via EventSource):**
```
event: service
data: {"type":"service","action":"update","id":"sv1aaa...","resource":{...}}

event: task
data: {"type":"task","action":"update","id":"tk001...","resource":{...}}

event: sync
data: {}
```

**Stacks summary:**
```json
{
  "@context": "/api/context.jsonld",
  "@type": "Collection",
  "items": [
    { "name": "webshop", "serviceCount": 6, "runningTasks": 12, "desiredTasks": 12, ... }
  ],
  "total": 3
}
```

## Notes

- The MSW service worker file (`mockServiceWorker.js`) must be served from the same origin as the iframe content
- `onUnhandledRequest: "bypass"` lets static assets (JS, CSS, fonts) pass through to the real server
- The demo build should set `<base href="/demo/">` so `apiPath()` detects the correct base
- MSW's SSE API may require the handler URL to match what `EventSource` opens — check that `apiPath("/events")` resolves to `/demo/events` and the MSW handler pattern matches it
- The dataset is pure TypeScript data — it should be roughly 500-800 lines (vs 1094 in Go, since TS object literals are more compact)
