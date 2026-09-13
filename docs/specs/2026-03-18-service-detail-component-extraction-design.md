# ServiceDetail Component Extraction

**Date:** 2026-03-18
**Status:** Approved

## Problem

`frontend/src/pages/ServiceDetail.tsx` is ~1950 lines. It contains the main page component plus 15+ named functions and components defined inline. This makes the file hard to navigate and violates the single-responsibility principle.

## Goal

Extract substantial components into a new `components/service-detail/` subdirectory, following the existing pattern used by `components/log/` and `components/metrics/`. No behavior changes — pure structural refactoring.

## File Structure

```
frontend/src/components/service-detail/
├── index.ts              # re-exports all public components
├── ServiceActions.tsx    # image update, rollback, restart buttons + popovers
├── ReplicaCard.tsx       # ReplicaDoughnut (private) + ReplicaCard with scale popover
├── EnvEditor.tsx         # env var CRUD table (draft state, JSON Patch save)
├── ResourcesEditor.tsx   # resource limits/reservations editor + ServiceResourceShape type
├── DeploymentChanges.tsx # last deployment spec diff display (props: changes + updateStatus)
└── PlacementPanel.tsx    # placement constraints + humanizeConstraint helper + PlacementShape type
```

`ReplicaDoughnut` moves into `ReplicaCard.tsx` as a private component — it is not exported from `index.ts`.

## What Stays in `ServiceDetail.tsx`

- `ServiceDetail` — the page component (main export)
- `serviceStatus` + `ServiceStatusCard` — helper and its only consumer
- `MountTypeBadge` — 15-line badge, used only in the mounts table
- `ResourcesPanel` + `ResourceLimitsBar` + `ResourceShape` type — used only in the Deploy Configuration collapsible section
- Pure helpers and their types: `UpdateConfigShape` + `updateConfigRows`, `cpuThresholds`, `memoryThresholds`, `formatCpu`

## Type Aliases That Move With Their Component

- `ServiceResourceShape` (line 1679) — used only by `ResourcesEditor`. Moves to `ResourcesEditor.tsx`.
- `PlacementShape` (line 1242) — used only by `PlacementPanel`. Moves to `PlacementPanel.tsx`.
- `ResourceShape` (line 1299) — used only by `ResourcesPanel` and `ResourceLimitsBar`, both of which stay. Stays in `ServiceDetail.tsx`.
- `UpdateConfigShape` (line 718) — used only by `updateConfigRows`, which stays. Stays in `ServiceDetail.tsx`.

## Spinner Consolidation

Replace all local `Spinner` definitions with imports of the shared `components/Spinner.tsx` (which wraps `<Loader2>` from lucide-react). Pass `className="size-3"` to match existing sizes. This is an intentional minor visual normalisation (Loader2 vs custom SVG arc).

**Files in `pages/` — import path `"../components/Spinner"`:**

| File | Current form | Action |
|---|---|---|
| `ServiceDetail.tsx` | named `Spinner` function (custom SVG) | replace with import |
| `NodeDetail.tsx` | named `Spinner` function (custom SVG) | replace with import |
| `TaskDetail.tsx` | named `Spinner` function (custom SVG) | replace with import |

**Files in `components/service-detail/` — import path `"../Spinner"`:**

| File | Current form | Action |
|---|---|---|
| `ServiceActions.tsx` (extracted) | inherits from `ServiceDetail.tsx` | add import |
| `EnvEditor.tsx` (extracted) | inherits from `ServiceDetail.tsx` | add import |
| `ResourcesEditor.tsx` (extracted) | inherits from `ServiceDetail.tsx` | add import |
| `ReplicaCard.tsx` (extracted) | anonymous inline `<svg>` block inside the `scaleLoading` conditional (not the permanent scale-trigger SVG icon, which stays) | replace with `<Spinner className="size-3" />` |

Note: `ReplicaCard` contains two SVGs. The permanent pencil/edit icon on the scale trigger button (lines 1091–1104) stays as-is. Only the conditional spinner SVG inside the "Scale" submit button (lines 1137–1156) is replaced.

## Conventions

- All extracted components use **named exports** (consistent with `log/` and `metrics/` subdirectories)
- `index.ts` re-exports everything so `ServiceDetail.tsx` imports from one path:
  ```ts
  import { ServiceActions, ReplicaCard, EnvEditor, ResourcesEditor, DeploymentChanges, PlacementPanel } from "../components/service-detail";
  ```
- Each extracted file imports only what it needs (types, API client, shared components)

## Out of Scope

- No behavior changes beyond the Spinner visual normalisation noted above
- No API or type changes
- No new abstractions or shared editor base components
