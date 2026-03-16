# Dashboard Guide

Cetacean's UI updates in real time. No refresh button, no polling interval, no "click to see latest." When a service
scales, a task fails, or a node goes down, you'll see it happen.

## Navigation

The top bar has everything:

- **Nav links** -- Nodes, Stacks, Services, Configs, Secrets, Networks, Volumes, Swarm, Topology
- **Search** -- `Cmd K` (Mac) or `Ctrl K` (Linux/Windows) opens the command palette
- **Theme toggle** -- cycles Light → Dark → System
- **Connection indicator** -- shows whether the SSE stream is healthy
- **User badge** -- your identity (hidden when auth is `none`)

On mobile, nav links collapse into a hamburger menu.

## Keyboard Shortcuts

Press `?` to see all shortcuts. The highlights:

| Shortcut           | Action                  |
|--------------------|-------------------------|
| `Cmd K` / `Ctrl K` | Open search palette     |
| `/`                | Focus search input      |
| `?`                | Show shortcuts help     |
| `Esc`              | Close overlay / go back |
| `g h`              | Go to cluster overview  |
| `g n`              | Go to nodes             |
| `g s`              | Go to services          |
| `g k`              | Go to stacks            |
| `g c`              | Go to configs           |
| `g x`              | Go to secrets           |
| `g w`              | Go to networks          |
| `g v`              | Go to volumes           |
| `g i`              | Go to swarm info        |
| `g t`              | Go to topology          |

In tables: `j`/`↓` to move down, `k`/`↑` to move up, `Enter` to open the selected row.

The `g` shortcuts are chords -- press `g`, release, then press the second key.

## Search

**Command palette** (`Cmd K`): type to search across all resource types. Results appear grouped by type (services first,
then stacks, nodes, tasks, configs, secrets, networks, volumes). Arrow keys to navigate, Enter to open, Esc to close.

Results show live state: a spinning orb for updating resources, colored dots for running/failed/completed. The palette
polls every 2 seconds to keep state indicators fresh without reordering results.

**Search page** (`/search?q=...`): the full-page version shows all results instead of the palette's top 3 per type.
Accessible via "View all results" in the palette, or by navigating directly.

Search matches against names, images, and labels across all resource types.

## List Pages

Every resource type has a list page with:

- **Search input** -- live, debounced substring filtering
- **Sortable columns** -- click a column header to sort, click again to reverse
- **View toggle** -- switch between table and grid layouts (persisted per resource type)
- **Real-time updates** -- rows update in place as SSE events arrive

Tables auto-virtualize above 100 rows, so even large clusters stay smooth.

Grid view shows resource cards with status badges, gauges, and metadata at a glance. Useful for nodes (CPU/memory
gauges) and services (replica health).

### Filtering

List pages support expr-lang filter expressions via the API. While the UI provides search-based filtering, the API's
`?filter=` parameter allows precise queries:

```
role == "manager" && state == "ready"          # nodes
name contains "web" && mode == "replicated"    # services
state == "failed" || error != ""               # tasks
scope == "swarm"                               # networks
services > 5                                   # stacks
```

See the [API reference](api.md) for filter fields per resource type.

## Detail Pages

Click any resource to see its detail page. Every detail page:

- Subscribes to its own SSE stream for real-time updates
- Shows cross-references (services using this config, tasks on this node, etc.)
- Includes a change history section

### Services

The service detail page is the most feature-rich:

- **Replica card** -- running/desired count with health indicator
- **Task table** -- every task with state, node, timestamps, and live metrics
- **Resource allocation** -- horizontal bar charts showing reserved vs. actual CPU/memory, with limit markers
- **Metrics charts** -- CPU and memory over time (requires [monitoring](monitoring.md))
- **Log viewer** -- live-tailing logs with search and filtering
- **Activity history** -- recent changes to this service

### Stacks

Stack detail shows everything deployed under that `com.docker.stack.namespace` label: services, configs, secrets,
networks, and volumes in separate tables. Each item links to its own detail page.

### Nodes

Node detail shows the tasks running on that node, resource gauges (CPU, memory, disk), and node metadata (role,
availability, engine version, address).

### Tasks

Task detail shows the container state, linked service and node, resource gauges for the running container, metrics
charts, and a log viewer scoped to that single task.

## Log Viewer

The log viewer appears on service and task detail pages. It supports:

- **Live tail** -- toggle the play button to stream logs in real time (pulsing green dot indicates streaming)
- **Time range** -- presets (1h, 6h, 24h, 7d) or custom datetime range
- **Stream filter** -- show all, stdout only, or stderr only
- **Level filter** -- all, error, warn, info, debug
- **Search** -- substring or regex, with match counter and next/previous navigation (`Enter`/`Shift+Enter`)
    - `Aa` toggles case sensitivity
    - `.*` toggles regex mode
- **Pin lines** -- click to pin up to 3 lines to the top of the viewer
- **JSON pretty-print** -- JSON log messages are automatically formatted
- **Copy & download** -- copy logs to clipboard or download as a `.log` file

Virtual scrolling handles large log volumes without breaking a sweat. "Load older" appears at the top when more history
is available.

## Charts & Metrics

Charts appear on the cluster overview, node detail, service detail, task detail, and stack detail pages. They
require [monitoring](monitoring.md) to be configured.

### Time Range

Every metrics panel has a range picker:

- **Presets** -- 1h, 6h, 24h, 7d
- **Custom range** -- click the calendar icon for a datetime range picker
- **Auto-refresh** -- toggleable, refreshes every 30 seconds
- **URL-persisted** -- `?range=1h` or `?from=...&to=...` so you can share links to specific time windows

### Interactions

**Click to isolate** -- click a series name or its line on the chart to dim everything else to 30% opacity. Click
again (or double-click) to restore all series.

**Linked crosshairs** -- hover over any chart and all sibling charts in the same panel show a synchronized vertical
crosshair with value dots. Useful for correlating CPU spikes with memory growth.

**Brush to zoom** -- click and drag horizontally to select a time range. The chart zooms in and the URL updates. The
5-pixel drag threshold prevents accidental zooms from clicks.

**Stacked area toggle** -- switch between line and stacked area views using the icons in the chart header. Stacked area
is useful for seeing total resource consumption across services.

**Stack drill-down** -- on the cluster overview, double-click a stack's series to drill into its individual services.
A "Back to all stacks" breadcrumb appears for navigation.

### Color Palette

Charts use a 10-color CVD-safe palette (IBM Carbon + Wong). The same resource always gets the same color across pages,
making it easy to track a service across different views.

Colors are defined as CSS custom properties (`--chart-1` through `--chart-10`), so they adapt to light and dark themes.

## Topology

The topology page (`/topology`) offers two views:

- **Logical** -- services grouped by stack, connected by overlay networks
- **Physical** -- tasks grouped by the nodes they're running on

Both are interactive graph visualizations. Click a service or node to navigate to its detail page.

## Themes

Click the theme toggle in the nav bar to cycle through:

- **Light** -- white background, dark text
- **Dark** -- dark gray background, light text
- **System** -- follows your OS preference

The current theme is persisted in localStorage. Theme transitions are smooth -- no flash of wrong colors.

## Real-Time Updates

Every page maintains its own SSE connection scoped to the resources it displays. When something changes:

- **List pages** -- rows are updated in place (no full reload). New resources appear, removed ones disappear, updated
  ones refresh their data.
- **Detail pages** -- the entire resource is re-fetched on change events.
- **Cluster overview** -- health cards, capacity bars, and activity feed update live.

The connection status indicator in the nav bar shows whether the SSE stream is healthy. If the connection drops,
Cetacean reconnects automatically.

## View Persistence

These preferences are saved in localStorage and survive page reloads:

| Setting              | Key                   | Values                    |
|----------------------|-----------------------|---------------------------|
| Table/grid toggle    | `viewMode:{resource}` | `table`, `grid`           |
| Theme                | `theme`               | `light`, `dark`, `system` |
| Collapsible sections | per section title     | open/closed state         |
