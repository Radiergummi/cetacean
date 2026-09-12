# Mobile / Responsive Layout Design

## Overview

Make the Cetacean dashboard fully usable on mobile devices (390px+ viewport width) with near-full feature parity. Uses a progressive enhancement approach — adding responsive behavior to existing components via Tailwind breakpoint classes and a `useMatchesBreakpoint` hook where CSS alone isn't sufficient.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Target viewport | 390px+ (large phones) | Covers modern iPhones and flagship Android |
| Tables on mobile | Force card view, hide toggle | Existing card components cover all list pages |
| Detail page order | Same as desktop | Consistency, no reordering complexity |
| Topology | Keep ReactFlow, adapt for touch | Enlarge tap targets, full-height container, collapsible legend |
| Navigation | Hamburger menu only | Already works, preserves vertical space |
| Chart interactions | Disable brush-to-zoom, keep tap-to-isolate | Range picker handles zoom; avoids gesture conflicts |
| Implementation strategy | Progressive enhancement | Minimal diff, no new abstractions, component-by-component |

## Breakpoint Convention

`md:` (768px) is the mobile/desktop boundary. Below `md` = mobile. This aligns with Tailwind defaults and the existing codebase usage.

## Shared Utilities

### `useMatchesBreakpoint(breakpoint, direction)` hook

A general-purpose hook for JS-level breakpoint checks. Looks up Tailwind's default breakpoint values (`sm: 640, md: 768, lg: 1024, xl: 1280, 2xl: 1536`) and constructs a `window.matchMedia` query. Returns a boolean.

```ts
const isMobile = useMatchesBreakpoint("md", "below");  // below 768px
const isDesktop = useMatchesBreakpoint("lg", "above");  // 1024px+
```

- `"below"` → `(max-width: <breakpoint - 1>px)`
- `"above"` → `(min-width: <breakpoint>px)`

Used by:

- `useViewMode` — force grid view below `md`
- `TimeSeriesChart` — disable brush-to-zoom below `md`
- Topology legend — switch to toggle button below `md`

## Component Changes

### 1. List Pages (Tables → Cards)

**useViewMode**: When `useMatchesBreakpoint("md", "below")` returns true, the hook returns `"grid"` regardless of localStorage. The setter still persists choices so desktop preference is remembered.

**ViewToggle**: Hidden on mobile via `hidden md:inline-flex` on the wrapper in `ListToolbar`.

**Grid layout**: Already responsive on all list pages (`grid-cols-1 sm:grid-cols-2 lg:grid-cols-3`). No changes needed to card grids.

**Affected pages**: NodeList, ServiceList, StackList, ConfigList, SecretList, NetworkList, VolumeList.

**TaskList**: Currently has no grid/card view — only table. On mobile this means horizontal scroll for a table with many columns (service, state, node, slot, etc.). Follow-up: create a `TaskCard` component to enable grid view on TaskList. For the initial pass, TaskList keeps horizontal scroll as a known limitation.

### 2. Charts & Metrics

**TimeSeriesChart**: In the `useMemo` building Chart.js options, check `useMatchesBreakpoint("md", "below")` and set `plugins.zoom.zoom.drag.enabled: false` on mobile. Tooltip, legend, crosshairs, and tap-to-isolate all remain.

**Linked crosshairs**: Keep — touch-move fires the same events as mouse-move in Chart.js.

**MetricsPanel grid**: Already `grid-cols-1 lg:grid-cols-2`. No change.

**NodeResourceGauges**: Add `flex-wrap` to the container. At 390px the 3 gauges (304px total) fit, but `flex-wrap` handles edge cases with longer labels.

**ResourceGauge sizes**: Keep current `size-20` (80px). Readable on phones.

### 3. Topology

**Container height**: On mobile, set `h-[calc(100dvh-3rem)]` on the ReactFlow container (full viewport minus header). The `3rem` offset should be verified against actual mobile header height during implementation. Using `100dvh` (dynamic viewport height) handles mobile browser address bar show/hide correctly. Eliminates scroll-vs-pan conflict.

**Tap targets**: Ensure minimum 44px touch targets on interactive elements within node cards (`ServiceCardNode`, `TaskCardNode`, `PhysicalNodeCard`). The cards themselves are already larger.

**Legend**: `StackLegend` is currently defined inline in `Topology.tsx` (not a separate component file). On mobile, replace the absolute-positioned legend with a toggle button (bottom-right info icon) that expands it as an overlay.

**No layout algorithm changes**: ELK computes positions dynamically; portrait orientation produces a taller/narrower graph which is acceptable.

### 4. Log Viewer

**LogTable**: Default `wrapLines` to `true` on mobile. Hide task ID column below `md` (truncated IDs are more useful on desktop).

**Log toolbar separators**: The separator divs live in `LogViewer.tsx` (not `LogToolbar.tsx`). Hide them on mobile with `hidden md:block` to prevent visual artifacts when toolbar items wrap on narrow screens. `flex flex-wrap` already handles the reflow.

**Log search input**: The search input in `LogViewer.tsx` is hardcoded to `w-56` (224px). Add `max-w-full` so it doesn't overflow the toolbar on narrow viewports: `w-56 max-w-full`.

**Container**: `overflow-auto` already handles scroll. No changes.

### 5. Search

**SearchPalette**: The modal container is currently `mx-auto mt-[15vh] max-w-lg`. On mobile, change to `mx-4 mt-[5vh] max-w-lg md:mx-auto md:mt-[15vh]` — `mx-4` provides edge padding on mobile while `md:mx-auto` restores centering on desktop. `max-w-lg` stays on both.

**GlobalSearch button**: Already responsive (icon-only on mobile, full bar on `xl`). No changes.

### 6. Header & Navigation

**ConnectionStatus**: Hide below `sm` (not `md` — intentionally narrower threshold since it only competes on very small screens).

**Hamburger button**: Bump padding from `p-2` to `p-2.5` for 44px touch target.

**Navigation links in dropdown**: Bump from `py-2` to `py-2.5` for 44px touch targets.

**No bottom tab bar**: Hamburger menu is sufficient.

### 7. Detail Pages

**Metadata grids**: Already `grid-cols-1 md:grid-cols-2 lg:grid-cols-3`. No changes.

**CollapsibleSection, InfoCard, SimpleTable, KVTable**: All flow naturally at narrow widths. No structural changes.

**Cluster overview**: Health cards `grid-cols-2 md:grid-cols-4` (two columns on mobile). Capacity/activity `grid-cols-1 md:grid-cols-2` (stacks). No changes.

### 8. General Touch & Spacing

**Touch targets**: Audit all interactive elements for 44px minimum. Key items: nav links in hamburger dropdown, theme toggle, icon buttons.

**Viewport meta**: Verify `viewport-fit=cover` for notched devices.

**Safe areas**: Add `env(safe-area-inset-bottom)` padding to main content for home indicator (iPhone gesture bar).

**No font size changes**: Current `text-sm`/`text-xs` are appropriate for data-dense dashboards.

**No gap scaling**: Current gaps work on mobile. Responsive gaps would be churn for marginal benefit.

## Out of Scope

- Phones below 390px width
- PWA / offline support
- Native app wrapper
- Orientation lock
- Mobile-specific routes or components (beyond the `useMatchesBreakpoint` hook)

## Files Affected

| File | Change |
|------|--------|
| New: `hooks/useMatchesBreakpoint.ts` | Shared breakpoint media query hook |
| `hooks/useViewMode.ts` | Mobile-aware default |
| `components/ListToolbar.tsx` | Hide ViewToggle on mobile |
| `components/metrics/TimeSeriesChart.tsx` | Disable brush-to-zoom on mobile |
| `components/metrics/NodeResourceGauges.tsx` | Add flex-wrap |
| `components/log/LogTable.tsx` | Default wrapLines on mobile, hide task ID column |
| `components/log/LogViewer.tsx` | Hide toolbar separators on mobile, constrain search input width |
| `components/search/SearchPalette.tsx` | Mobile margin/padding |
| `pages/Topology.tsx` | Full-height container on mobile, StackLegend toggle button |
| `App.tsx` | Touch target sizes, hide ConnectionStatus on small screens |
| `index.html` | Verify viewport meta |
