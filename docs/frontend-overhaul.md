# Frontend Overhaul Plan

Comprehensive plan for improving the Cetacean frontend from a basic prototype to a polished observability dashboard, informed by UX patterns from Grafana, Datadog, Kibana, Sentry, Dozzle, and Portainer.

## 1. Log Viewer -- DONE

### 1.1 Log Viewer Component (`components/LogViewer.tsx`) -- DONE
- [x] **Structured log display**: Table layout with line numbers, timestamps, severity bars, message body
- [x] **Parse Docker log format**: RFC3339 timestamp split from message, displayed in separate columns
- [x] **JSON detection and formatting**: Auto-detect and pretty-print JSON log lines (emerald colored)
- [x] **Line numbers**: Gutter column with line numbers
- [x] **Auto-scroll**: Pin to bottom with "Jump to bottom" pill when scrolled up
- [x] **Wrap toggle**: Toolbar button toggles `whitespace-pre` / `whitespace-pre-wrap`
- [x] **Severity detection**: error/warn/info/debug detected from log content, colored left-edge bars + message tinting

### 1.2 Log Search and Filtering -- DONE
- [x] **Search within logs**: Filter input highlights matches (yellow) and shows match count (52/108)
- [x] **Case sensitivity toggle**: "Aa" button in search bar
- [x] **Clear button**: X button to clear search
- [x] **Keyboard shortcut**: Ctrl+F focuses search when log viewer is focused
- [ ] **Regex toggle**: Button to enable regex search mode
- [ ] **Stream filter**: Toggle buttons for stdout/stderr filtering

### 1.3 Log Controls -- DONE
- [x] **Tail line count selector**: Dropdown (100, 500, 1000, 5000)
- [x] **Refresh button**: Manual reload
- [x] **Copy button**: Copy filtered log lines to clipboard
- [x] **Download button**: Download as `.log` file
- [ ] **Clear button**: Clear the current log view without refetching

### 1.4 Log Loading UX -- DONE
- [x] **Remove "View Logs" gate**: Logs load automatically on mount
- [x] **Loading state**: Static gray placeholder block (400px height)
- [x] **Error state**: Red-tinted alert with "Failed to load logs" + Retry button
- [x] **Empty state**: Centered "No logs available" message

### 1.5 Backend: Log Streaming (optional enhancement)
- [ ] **SSE-based log streaming endpoint**: `GET /api/services/{id}/logs/stream` with `Follow: true`
- [ ] **Auto-pause**: Stop consuming stream when user scrolls away from bottom

---

## 2. Charts and Metrics -- MOSTLY DONE

### 2.1 Fix "No Data" Issue -- DONE
- [x] **Fixed Prometheus config**: Switched from broken `dockerswarm_sd_configs` (permission denied) to `dns_sd_configs` for cAdvisor and node-exporter discovery
- [x] **Fixed node detail queries**: Removed instance filtering (doesn't match in OrbStack), use unfiltered queries for single-node setup
- [x] **Fixed service detail queries**: Updated to use `id=~"/docker/.+"` filter since OrbStack cAdvisor doesn't expose Docker labels
- Note: Per-service metrics require cAdvisor with Docker label export (works in production, not on OrbStack)

### 2.2 Chart States -- DONE
- [x] **Loading state**: Static gray placeholder block (200px, `bg-muted/50`)
- [x] **Error state**: Red-tinted alert with destructive colors + Retry button (RefreshCw icon)
- [x] **No data state**: Centered muted message with BarChart3 icon: "No data for this time range"

### 2.3 Chart Interactivity -- PARTIAL
- [x] **Responsive resize**: ResizeObserver re-renders chart on container resize
- [x] **Y-axis formatting**: Bytes (KB/MB/GB), percentages (%), cores (3 decimals)
- [x] **Better colors**: Indigo primary with area fill, 6-color palette for multi-series
- [x] **Legend**: Series labels shown below chart (via uPlot)
- [ ] **Tooltips**: Crosshair cursor showing all series values at hovered timestamp
- [ ] **Legend click-to-isolate**: Click a label to isolate that series

### 2.4 MetricsPanel Improvements
- [ ] **Synchronized time range**: Vertical cursor line across sibling charts on hover
- [ ] **Collapse/expand**: Allow collapsing the MetricsPanel section
- [ ] **Time range in URL**: Persist in URL query params

---

## 3. Visual Polish and UX Foundations -- PARTIAL

### 3.1 Loading States
- [x] **ClusterOverview skeleton**: 8 gray placeholder cards in 2x4 grid while loading
- [ ] **List page skeletons**: Skeleton table rows for list pages
- [ ] **Detail page skeletons**: Skeleton info cards + table for detail pages
- [ ] **Reusable Skeleton component**: `<Skeleton width height shape>` component

### 3.2 Error States -- DONE
- [x] **Error boundary**: Wrap routes in error boundary with friendly "Something went wrong" page
- [x] **FetchError component**: Reusable red-tinted alert banner + error message + Retry button
- [x] **Retry in useSwarmResource**: Add `retry()` to hook return value

### 3.3 Empty States -- DONE
- [x] **Empty table component**: Centered icon + "No results" + guidance text when table has 0 rows
- [ ] **Empty detail sections**: Show muted line instead of hiding empty sections

### 3.4 Navigation -- DONE
- [x] **Active route highlighting**: Current nav link is bold/dark, others muted. Uses `useLocation()` with prefix matching for detail pages
- [x] **Breadcrumbs**: `PageHeader` component with breadcrumbs on all detail pages (ServiceDetail, NodeDetail, StackDetail)
- [x] **Back navigation**: Breadcrumb links serve as back navigation

### 3.5 Table UX Improvements -- MOSTLY DONE
- [x] **Full-row clickability**: ServiceList, NodeList, and StackList have clickable rows with `cursor-pointer hover:bg-muted/50`
- [x] **Sortable columns**: Sort indicators (chevron) on column headers, click to toggle (ServiceList, NodeList, StackList)
- [ ] **Status indicator bar**: 3px colored left border on table rows with status
- [ ] **Sticky table headers**: `sticky top-0` on `<thead>`

### 3.6 Stat Cards (ClusterOverview) -- MOSTLY DONE
- [x] **Color thresholds**: Red-tinted background for failed/down cards when value > 0 (with dark mode variant)
- [x] **Clickable tiles**: Nodes Ready/Down → /nodes, Services → /services, Stacks → /stacks
- [x] **Tabular nums**: Numbers use `tabular-nums` for consistent alignment
- [ ] **Trend indicator**: Up/down arrow for changed values (requires tracking previous snapshot)

---

## 4. Dark Mode -- DONE

### 4.1 Theme Toggle -- DONE
- [x] **Dark mode toggle button**: Sun/moon icon in navbar (ThemeToggle component)
- [x] **System preference detection**: Defaults to `prefers-color-scheme`, stores preference in `localStorage`
- [x] **Implementation**: `useEffect` toggles `.dark` class on `<html>`

### 4.2 Dark Mode Fixes -- DONE
- [x] **Chart colors**: Updated to use named colors (#6366f1 indigo etc.) with subtle grid lines (#88888820) — works in both themes
- [x] **Status badge contrast**: Added `dark:` variants for all states (green, red, yellow, gray) in TaskStatusBadge
- [x] **Log viewer**: Dark terminal background (bg-gray-950) works naturally in both themes
- [x] **Table row highlights**: Added `dark:bg-red-950/30` for failed task card tinting
- [x] **Stat card tinting**: `dark:border-red-900` for red-tinted stat cards

---

## 5. Component Consolidation -- MOSTLY DONE

### 5.1 Deduplicate Components -- DONE
- [x] **Unified TaskStatusBadge**: Single component with `STATE_COLORS` map handles task states (running, failed, preparing, shutdown) AND node states (ready, down, pending). Removed duplicate `StatusBadge` from NodeList
- [x] **InfoCard**: Shared component used by ServiceDetail and NodeDetail

### 5.2 Shared Patterns -- MOSTLY DONE
- [x] **PageHeader component**: `<PageHeader title breadcrumbs>` used across all 11 pages (7 list + 3 detail + overview)
- [x] **EmptyState component**: Reusable centered empty state with icon
- [x] **FetchError component**: Reusable error alert with retry button
- [x] **SortableHeader component**: Reusable sortable column header with indicators
- [ ] **DataTable component**: Extract table wrapper pattern (overflow, border, sticky header)

---

## 6. Minor UX Improvements -- TODO

### 6.1 Service Detail
- [ ] **Labels section**: Show service labels as key-value badges (collapsible if >5)
- [ ] **Environment hints**: Show constraint info (placement constraints) if present
- [ ] **Update status detail**: Show started/completed timestamps and message, not just state

### 6.2 Node Detail
- [ ] **Resource info**: Show CPU count and memory from `Description.Resources`
- [ ] **Manager status**: Show leader/reachability info if `ManagerStatus` exists

### 6.3 Stack Detail
- [ ] **Resource counts in header**: "3 services, 1 config, 2 secrets" summary
- [ ] **Service health**: Running/total task counts next to each service

### 6.4 Timestamps
- [ ] **Relative timestamps**: "2 minutes ago" with full date on hover (`title` attribute)

### 6.5 URL State
- [ ] **Search in URL**: `?search=nginx` for shareable filtered views
- [ ] **Sort in URL**: Column and direction in URL params

---

## Infrastructure Fixes (done alongside frontend work)

- [x] **Prometheus service discovery**: Switched `prometheus.yml` from `dockerswarm_sd_configs` (requires Docker socket access, fails with permission denied on non-root Prometheus) to `dns_sd_configs` using Swarm DNS (`tasks.cadvisor`, `tasks.node-exporter`)
- [x] **useSwarmResource type fix**: Cast `event.resource` as `T` to fix TypeScript error with `unknown` type from SSE events

---

## What's Left (in priority order)

1. **Empty detail sections** (3.3) — show muted line instead of hiding empty sections
2. **Relative timestamps** (6.4) — "2 minutes ago" with hover
3. **URL state** (6.5) — search and sort in URL params
4. **Detail page enrichment** (6.1-6.3) — labels, resources, manager status, stack health
5. **Chart tooltips** (2.3) — crosshair cursor with formatted values
6. **MetricsPanel improvements** (2.4) — collapse, URL time range, synchronized hover
7. **DataTable component** (5.2) — extract table wrapper pattern
8. **Log streaming** (1.5) — SSE-based live tail (backend + frontend)
9. **List page skeletons** (3.1) — skeleton table rows for loading states
10. **Status indicator bar** (3.5) — 3px colored left border on table rows
11. **Sticky table headers** (3.5) — `sticky top-0` on `<thead>`
