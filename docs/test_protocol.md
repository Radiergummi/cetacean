# Manual Test Protocol

Comprehensive checklist for verifying all pages and interactions in the Cetacean dashboard. Each section lists the route, what to verify, and specific interactions to exercise.

Prerequisites: a running Swarm cluster with at least 2 nodes, several stacks deployed (services, configs, secrets, networks, volumes), and optionally Prometheus + node-exporter + cAdvisor for metrics coverage.

---

## Global Shell

### Navigation Bar
- [ ] Logo links to `/`
- [ ] All nav links (Nodes, Stacks, Services, Tasks, Configs, Secrets, Networks, Volumes, Swarm, Topology, Metrics) navigate correctly and show active indicator
- [ ] Connection status indicator shows green when SSE is connected
- [ ] Disconnect the backend — indicator turns to disconnected state; reconnect — recovers
- [ ] Theme toggle switches between light and dark mode
- [ ] Recommendations indicator shows badge count; click navigates to `/recommendations`
- [ ] Mobile: hamburger menu expands/collapses nav links

### Keyboard Shortcuts
- [ ] Press `?` — shortcuts overlay opens
- [ ] Press `?` or `Esc` while overlay is open — overlay closes
- [ ] Navigation chords listed in overlay work: `g h` (home), `g n` (nodes), `g s` (services), `g a` (tasks), `g k` (stacks), `g c` (configs), `g x` (secrets), `g w` (networks), `g v` (volumes), `g i` (swarm), `g t` (topology)
- [ ] Navigation chords NOT listed in overlay still work: `g r` (recommendations), `g m` (metrics)
- [ ] List keyboard nav: `j`/`↓` selects next row, `k`/`↑` selects previous, `Enter` opens selected

### Search
- [ ] Press `/` — search input in nav bar focuses
- [ ] Press `Cmd+K` — search palette opens
- [ ] Type a query — results appear grouped by type (services, stacks, nodes, tasks, configs, secrets, networks, volumes)
- [ ] Arrow keys navigate results; `Enter` navigates to selected result
- [ ] `Esc` closes palette (or goes back one step when in action mode)
- [ ] Results show state indicators (color orbs for services/tasks)
- [ ] "View all results" link navigates to `/search?q=...`
- [ ] Results poll every 2s for state updates without reordering

### Search Palette Actions
- [ ] Type "scale" — action suggestion row appears; select it → pick a service → enter replica count → executes
- [ ] Type "image" — pick a service → enter new image tag → executes
- [ ] Type "rollback" — pick a service → confirmation → executes
- [ ] Type "restart" — pick a service → confirmation → executes
- [ ] Type "drain" — pick a node → confirmation → executes
- [ ] Type "activate" — pick a node → executes (no confirmation)
- [ ] Type "pause" — pick a node → confirmation → executes
- [ ] Type "promote" — pick a node → executes
- [ ] Type "demote" — pick a node → confirmation → executes
- [ ] Type "kill task" — pick a task → confirmation → executes
- [ ] Type "remove service" — pick a service → confirmation → executes
- [ ] Type "remove node" — pick a node → confirmation → executes
- [ ] Type "remove stack" — pick a stack → confirmation → executes
- [ ] Type "remove config" — pick a config → confirmation → executes
- [ ] Type "remove secret" — pick a secret → confirmation → executes
- [ ] Type "remove network" — pick a network → confirmation → executes
- [ ] Type "remove volume" — pick a volume → confirmation → executes
- [ ] Type "shortcuts" — immediately opens shortcuts overlay
- [ ] `Backspace` on empty input in action mode — goes back one step
- [ ] Destructive actions show confirmation bar with Cancel/Confirm

---

## Cluster Overview (`/`)

- [ ] Page loads without errors
- [ ] Health cards show correct counts for Nodes, Services, Failed Tasks, Tasks
- [ ] Each health card links to the corresponding list page
- [ ] Failed Tasks card shows trend delta indicator
- [ ] Capacity section is collapsible; shows cluster utilization bars
- [ ] Recommendation summary appears in capacity section (if recommendations exist)
- [ ] Recent Activity section is collapsible; shows last 25 change events
- [ ] SSE updates: deploy a new service — health cards and activity feed update in real time
- [ ] **With Prometheus**: MonitoringStatus banner shows healthy state; dismiss button hides it (persisted in localStorage)
- [ ] **With Prometheus**: Resource Usage by Stack charts render; double-click a stack to drill down to per-service view; `Esc` exits drill-down; toggleable legend with show/hide button and click-to-isolate per series; "Show all / Top 10 only" toggle when many series
- [ ] **Without Prometheus**: MonitoringStatus banner shows setup guidance

---

## List Page Pagination and Infinite Scroll

All resource list pages (Nodes, Services, Tasks, Configs, Secrets, Networks, Volumes, Stacks) use HTTP Range Request pagination with infinite scroll.

- [ ] API requests include `Range: items 0-49` header (verify in browser DevTools Network tab)
- [ ] API responses include `Accept-Ranges: items` header
- [ ] For partial responses: status is `206 Partial Content` with `Content-Range: items 0-49/N` header
- [ ] For full responses (< 50 items): status is `200 OK`
- [ ] **Infinite scroll**: scroll to the bottom of a list with more than 50 items — "Loading..." sentinel appears; new items load automatically
- [ ] Infinite scroll works across multiple pages (scroll through 100+ items without gaps or duplicates)
- [ ] Changing search query or sort resets to first page (no stale data from previous query)
- [ ] SSE updates: add a resource externally — total count increments; existing items update in-place
- [ ] SSE removes: remove a resource externally — item disappears from list; total count decrements

---

## Node List (`/nodes`)

- [ ] Table renders with columns: Hostname, Role, Availability, Status, Address, Engine
- [ ] Search filters nodes by hostname
- [ ] Click column headers to sort; URL params `?sort=` and `?dir=` update
- [ ] Table/grid view toggle works; preference persists across navigation
- [ ] Row click navigates to node detail
- [ ] Grid cards render correctly
- [ ] **With Prometheus**: CPU%, Memory%, CPU sparkline columns appear; cluster-level gauge row shows above the list; MetricsPanel with CPU/Memory charts renders

## Node Detail (`/nodes/:id`)

- [ ] Metadata grid shows hostname, role, availability, status, address, engine version
- [ ] **Resource gauges** (CPU, Memory, Disk) render if Prometheus is configured
- [ ] **Availability editor**: click pencil icon → popover with Active/Pause/Drain options; selecting Drain triggers confirmation dialog; change applies and UI updates
- [ ] **Role editor**: click pencil icon → popover with Worker/Manager options; shows quorum warning; change applies
- [ ] **Labels editor**: click Edit → add a label → Save; edit an existing label → Save; delete a label → Save; reserved keys (e.g. `com.docker.stack.namespace`) are read-only
- [ ] Tasks table shows tasks on this node with correct state indicators
- [ ] **With Prometheus**: MetricsPanel with CPU, Memory, Disk I/O, Network I/O charts renders; range picker works (1H/6H/24H/7D)
- [ ] **With cAdvisor**: "Resource Usage by Stack" section shows CPU and Memory drill-down charts per stack; double-click a stack to see per-service breakdown
- [ ] **Docker Disk Usage section**: shows disk usage breakdown (images, containers, volumes, build cache) with doughnut chart when available
- [ ] ActivitySection shows recent change events
- [ ] **Remove button**: only enabled when node state is `down`; confirmation dialog requires typing hostname; removes node and navigates back
- [ ] **Force remove**: if normal remove fails with a specific error, a "Force remove" button appears inline; click force-removes the node
- [ ] SSE: change node availability externally — page updates in real time

---

## Service List (`/services`)

- [ ] Table renders with columns: Name, Image, Mode, Ports, Replicas, Status
- [ ] Search filters services
- [ ] Sortable columns work
- [ ] Table/grid view toggle works
- [ ] Row click navigates to service detail
- [ ] **With cAdvisor**: CPU and Memory sparkline columns appear; MetricsPanel above list renders top-10 charts
- [ ] **With recommendations**: sizing badges appear on relevant services

## Service Detail (`/services/:id`)

### Header and Actions
- [ ] Page loads with correct service name, image, replica count, status
- [ ] **Rollback button**: disabled if no previous spec; click opens confirmation; executes rollback
- [ ] **Restart button**: click opens confirmation; executes restart
- [ ] **Remove button**: click opens confirmation; removes service; navigates to stack or `/services`

### Sizing Banner
- [ ] Shows per-service recommendations if any exist
- [ ] "Apply suggested value" button applies the fix

### Tasks and Metrics
- [ ] Tasks table shows all tasks for this service with state indicators
- [ ] Tasks table has state filter (segmented control with badge counts per state); filter auto-resets when selected state has no tasks
- [ ] **With cAdvisor**: CPU/Memory sparklines in task rows; MetricsPanel with CPU/Memory charts; charts show threshold lines for limits

### Last Deployment
- [ ] Section appears if spec changes exist; shows diff view
- [ ] Section is collapsible

### Recent Activity
- [ ] Section appears if history entries exist; shows activity feed
- [ ] Section is collapsible

### Container Configuration (collapsible)
- [ ] **Command editor**: edit command, entrypoint, args → Save
- [ ] **Runtime editor**: edit user, working directory → Save
- [ ] **Capabilities editor**: add/remove Linux capabilities → Save
- [ ] **Extra hosts editor**: add/remove /etc/hosts entries → Save
- [ ] **DNS editor**: edit nameservers, search domains, resolver options → Save

### Inline Editors
- [ ] **Environment variables**: add a var → Save; edit a var → Save; delete a var → Save; Cancel discards changes; `Esc` key also cancels edit mode
- [ ] **Labels**: add/edit/delete labels; integration-prefixed labels are filtered into their own panels; reserved keys are read-only
- [ ] **Healthcheck**: edit test command, interval, timeout, retries, start period → Save; timeline visualization updates
- [ ] **Ports**: add/edit/delete published ports → Save
- [ ] **Mounts**: add/edit/delete volume/bind/tmpfs mounts → Save
- [ ] **Networks**: add/remove network attachments with aliases → Save
- [ ] **Configs**: add/remove config file mounts → Save
- [ ] **Secrets**: add/remove secret file mounts → Save

### Deploy Configuration (collapsible, closed by default)
- [ ] **Endpoint mode**: switch between VIP and DNSRR → Save
- [ ] **Resources**: edit CPU/Memory reservations and limits → Save; allocation bars update
- [ ] **Placement**: edit constraints and preferences → Save
- [ ] **Restart policy**: read-only display of condition, delay, max attempts, window
- [ ] **Log driver**: select driver, configure options → Save
- [ ] **Update policy**: edit parallelism, delay, failure action, monitor, max failure ratio, order → Save
- [ ] **Rollback policy**: same fields → Save

### Integration Panels
- [ ] **Traefik panel**: appears if traefik labels exist; shows routers, services, middlewares
- [ ] **Shepherd panel**: appears if shepherd labels exist
- [ ] **Cronjob panel**: appears if swarm-cronjob labels exist
- [ ] **Diun panel**: appears if diun labels exist

### Log Viewer
- [ ] See [Log Viewer](#log-viewer) section below

### Real-Time Updates
- [ ] SSE: scale the service externally — replica count and tasks table update
- [ ] SSE: deploy a new image — status card updates; Last Deployment section appears

---

## Service Sub-Resource (`/services/:id/:subResource`)

- [ ] Navigate to e.g. `/services/:id/env` — shows key-value table of env vars
- [ ] Breadcrumb links back to service detail
- [ ] Invalid sub-resource redirects to service detail
- [ ] Error state shows retry button

---

## Task List (`/tasks`)

- [ ] Table renders with columns: Service, State, Desired, Node, Slot, Image
- [ ] Search filters tasks
- [ ] Sortable columns (State, Node) work
- [ ] Row click navigates to task detail
- [ ] **With cAdvisor**: CPU/Memory sparklines appear for running tasks

## Task Detail (`/tasks/:id`)

- [ ] Metadata shows state, desired state, service link, node link, slot, image, timestamp, container ID
- [ ] Error message and exit code shown if present
- [ ] **Remove button**: opens confirmation; removes task; navigates to parent service
- [ ] **With cAdvisor**: resource gauges and MetricsPanel with CPU/Memory charts
- [ ] Log viewer renders (see [Log Viewer](#log-viewer) section)

---

## Stack List (`/stacks`)

- [ ] Table renders with columns: Name (with health dot), Tasks, Services, Status
- [ ] Search filters stacks
- [ ] Table/grid view toggle works
- [ ] Grid cards show task progress bar, resource bars, resource count pills
- [ ] Row click navigates to stack detail

## Stack Detail (`/stacks/:name`)

- [ ] Header shows stack name with resource count subtitle
- [ ] **Remove button**: opens confirmation requiring stack name to be typed; shows resource counts; removes stack
- [ ] Services section: table with Name (link), Image, Mode, Tasks (running/desired with health color)
- [ ] Configs section: list of links to config detail pages; scrollable if many
- [ ] Secrets section: list of links to secret detail pages
- [ ] Networks section: Name (link) and Driver columns
- [ ] Volumes section: list of links to volume detail pages
- [ ] All sections are collapsible
- [ ] SSE: scale a service in the stack — task counts update (debounced)

---

## Config List (`/configs`)

- [ ] Table renders with columns: Name, Created
- [ ] Search filters configs
- [ ] Table/grid view toggle works
- [ ] Row click navigates to config detail

## Config Detail (`/configs/:id`)

- [ ] Metadata shows ID (copyable), Stack link, Created, Updated
- [ ] **Labels editor**: add/edit/delete labels → Save
- [ ] **Data section**: decoded content in code block; Copy button copies to clipboard
- [ ] "Used by Services" table with links
- [ ] Activity section shows change history
- [ ] **Remove button**: disabled if in use by services (tooltip explains); confirmation dialog

---

## Secret List (`/secrets`)

- [ ] Table renders with columns: Name, Created
- [ ] Search, table/grid toggle work
- [ ] Row click navigates to secret detail

## Secret Detail (`/secrets/:id`)

- [ ] Metadata shows ID (copyable), Stack link, Created, Updated
- [ ] **Labels editor**: add/edit/delete labels → Save
- [ ] "Used by Services" table with links
- [ ] Activity section shows change history
- [ ] **Remove button**: disabled if in use; confirmation dialog

---

## Network List (`/networks`)

- [ ] Table renders with columns: Name, Driver, Scope, Created
- [ ] Search, table/grid toggle work
- [ ] Row click navigates to network detail

## Network Detail (`/networks/:id`)

- [ ] Metadata shows ID, Driver, Scope, Stack link, Created
- [ ] Network flags shown as badges: Internal, Attachable, Ingress, IPv6
- [ ] IPAM Configuration section: Subnet, Gateway, IP Range per config block
- [ ] Driver Options section: key-value pills
- [ ] Labels displayed (stack label filtered out)
- [ ] "Connected Services" table with links
- [ ] Activity section
- [ ] **Remove button**: disabled if in use; confirmation dialog

---

## Volume List (`/volumes`)

- [ ] Table renders with columns: Name, Driver, Scope, Mountpoint
- [ ] Search, table/grid toggle work
- [ ] Row click navigates to volume detail

## Volume Detail (`/volumes/:name`)

- [ ] Metadata shows Driver, Scope, Stack link, Created, Mountpoint
- [ ] Driver Options section
- [ ] Labels displayed
- [ ] "Mounted by Services" table with links
- [ ] Activity section
- [ ] **Remove button**: disabled if in use; "Force remove" fallback on error; confirmation dialog

---

## Plugin List (`/plugins`)

- [ ] Table renders with plugin name, enabled status
- [ ] "Install Plugin" button opens dialog (requires impactful operations level): enter remote reference → "Check Privileges" button fetches required privileges → privileges table shown → "Install" button installs the plugin
- [ ] Row click navigates to plugin detail

## Plugin Detail (`/plugins/:name`)

- [ ] Metadata shows ID, Description, Reference image, Docker version, Status (enabled/disabled), Type
- [ ] **Enable button** (when disabled): click enables the plugin
- [ ] **Disable button** (when enabled): click opens confirmation; disables the plugin
- [ ] **Upgrade button**: opens InstallPluginDialog in upgrade mode
- [ ] **Remove button**: click opens confirmation; removes plugin; navigates to `/plugins`
- [ ] **Settings section**: Args editor (free-text input), Env editor (key-value pairs)
- [ ] **Configuration section** (read-only): entrypoint, working dir, user, network type, interface socket, mounts table, capabilities list, devices table

---

## Topology (`/topology`)

### Logical View
- [ ] Services grouped by stack render as nodes on a canvas
- [ ] Network edges connect services; colored by network
- [ ] Hover a network edge — edge highlights; network name labels appear
- [ ] Click a network label on a hovered edge — navigates to network detail
- [ ] Nodes are draggable
- [ ] Click a service node — navigates to service detail
- [ ] Stack legend shows stack-to-color mapping; collapsible on mobile
- [ ] Canvas fits to view on load
- [ ] Canvas supports panning and zooming
- [ ] SSE: add/remove a service — layout updates (2s debounce)
- [ ] Error state shows Retry button

### Physical View
- [ ] Switch to Physical tab via segmented control
- [ ] Nodes show tasks grouped by Docker node
- [ ] Click a service card within a node — navigates to service detail
- [ ] Hover a task card — highlights the parent service
- [ ] Nodes are draggable
- [ ] Canvas fits to view

### Export Formats (API)
- [ ] `curl -H "Accept: application/vnd.jgf+json" /topology` — returns JGF document with two graphs (`network` and `placement`); Content-Type is `application/vnd.jgf+json`
- [ ] `curl -H "Accept: application/graphml+xml" /topology` — returns valid GraphML XML (network graph only); Content-Type is `application/graphml+xml`
- [ ] `curl -H "Accept: text/vnd.graphviz" /topology` — returns valid DOT format (network graph only); Content-Type is `text/vnd.graphviz`
- [ ] Extension suffixes work: `/topology.jgf`, `/topology.graphml`, `/topology.dot`
- [ ] GraphML contains service nodes with metadata (label, replicas, image, mode), edges with network names, and stack subgraphs
- [ ] DOT contains service nodes with attributes, `subgraph cluster_<stack>` grouping, and `--` edges with network labels
- [ ] JGF network graph uses `urn:cetacean:service:<id>` URNs for node keys and hyperedges for stack membership
- [ ] JGF placement graph uses `urn:cetacean:node:<id>` and `urn:cetacean:service:<id>` URNs with task data in hyperedge metadata
- [ ] Deprecated endpoints `/topology/networks` and `/topology/placement` return `Deprecation: true` and `Link: </topology>; rel="successor-version"` headers
- [ ] ETag caching works on all topology export formats (same data → same ETag)

---

## Swarm (`/swarm`)

### Metadata
- [ ] Shows Cluster ID, Created, Updated, Default Address Pool, Subnet Size, Data Path Port

### Join Commands
- [ ] "Join as Manager" button opens dialog with `docker swarm join` command; Copy button works
- [ ] "Join as Worker" button opens similar dialog

### Token Rotation
- [ ] "Rotate Worker Token" button opens confirmation; executes rotation
- [ ] "Rotate Manager Token" button opens confirmation; executes rotation

### Editable Panels
- [ ] **Raft** (configuration level): edit Snapshot Interval, Keep Old Snapshots, Log Entries for Slow Followers → Save; Election Tick and Heartbeat Tick are read-only
- [ ] **CA Configuration** (impactful level): edit Node Certificate Expiry → Save; "Force Rotate" button with confirmation
- [ ] **Orchestration** (configuration level): edit Task History Retention Limit → Save
- [ ] **Dispatcher** (configuration level): edit Heartbeat Period → Save
- [ ] **Encryption** (impactful level): toggle Auto-Lock Managers; when enabled: Show/Hide Unlock Key (with Copy button when visible), Rotate Unlock Key (confirmation), Unlock Swarm (token input dialog)
- [ ] Verify panels are read-only at insufficient operations level

### Other
- [ ] Task Defaults section (if log driver configured): read-only
- [ ] Plugins section with "Install Plugin" button

---

## Metrics Console (`/metrics`)

- [ ] MonitoringStatus banner shows Prometheus status
- [ ] Query input with PromQL autocomplete (label names and values)
- [ ] Type a query and press Enter or click Run — chart and result table render
- [ ] Query persisted in URL (`?q=`); auto-runs on page load if present
- [ ] Range segmented control (1H/6H/24H/7D) changes chart time range; persisted as `?range=` URL param
- [ ] Chart supports click-to-isolate, linked crosshairs, and brush-to-zoom (no pause/resume or stacked area toggle on this page — those are MetricsPanel-only)
- [ ] QueryResultTable shows instant query results below the chart

---

## Recommendations (`/recommendations`)

- [ ] Filter tabs: All, Sizing, Config, Operational, Cluster; active tab updates `?filter=` URL param
- [ ] Recommendation cards show severity icon, message, target link
- [ ] Click chevron to expand/collapse detail explanation
- [ ] Target name links to service or node detail page
- [ ] "Apply suggested value" button: shows spinner during execution; on success card is dismissed; on failure error message appears at top
- [ ] Empty state shown when no recommendations match the filter

---

## Log Viewer

Embedded in Service Detail and Task Detail pages.

### Controls
- [ ] Collapse/expand the section (persisted in localStorage)
- [ ] Live indicator pulses green when tailing
- [ ] **Time range selector**: preset buttons (All, Last 5m, Last 15m, Last 1h, Last 6h, Last 24h, Last 7d); custom from/until datetime inputs with Apply
- [ ] Refresh button triggers manual fetch
- [ ] Live tail toggle (Play/Stop) starts/stops SSE streaming
- [ ] Wrap lines toggle
- [ ] Stream filter: All / stdout / stderr
- [ ] Level filter: All / Error / Warn / Info / Debug
- [ ] Copy button copies filtered lines with timestamps to clipboard
- [ ] Download button saves as `.log` file
- [ ] Clear button clears all lines and pinned lines
- [ ] Fullscreen button enters/exits browser fullscreen

### Search
- [ ] Type in search input — matching text highlighted in log lines
- [ ] Case-sensitive toggle changes matching behavior
- [ ] Regex toggle enables regex patterns
- [ ] Match count displayed; prev/next arrows navigate between matches; current match scrolls into view

### Table Interactions
- [ ] Click a JSON log line — expands/collapses pretty-printed view
- [ ] Hover a row — pin button appears; click pins line to top (up to 3); pinned lines show unpin button
- [ ] Task ID cell (service logs only) — click filters to that task's logs; filter chip appears with X to clear
- [ ] Log level color bar on left border matches detected level

### Pagination and Following
- [ ] "Load older" button appears when more history exists; click loads previous page
- [ ] "Load newer" button appears when newer logs exist
- [ ] Follow/Unfollow toggle: auto-scrolls to bottom when following
- [ ] Resize handle at bottom: drag to resize; double-click resets to default height

---

## Metrics Panel Interactions

Shared by Cluster Overview, Node List, Service List, Node Detail, Service Detail, Task Detail.

- [ ] Range picker: 1H / 6H / 24H / 7D buttons; live ranges stream via SSE
- [ ] Custom date range: calendar icon opens picker with quick presets (Last 2h, Last 12h, Last 48h, Last 3d) plus from/until datetime inputs; Apply sets range; Clear removes it; persisted as `?from=&to=` URL params
- [ ] Refresh button triggers manual refetch
- [ ] Pause/Resume live streaming toggle
- [ ] Stacked area toggle (where available): switches between line and area chart modes
- [ ] **Click-to-isolate**: single click a series — other series dim to 30%; click again restores
- [ ] **Brush-to-zoom**: horizontal drag selects a time range (5px threshold); releases zooms in
- [ ] **Linked crosshairs**: hover on one chart — vertical dashed line + dots appear on sibling charts
- [ ] **Tab visibility**: switching to another browser tab pauses SSE streaming; switching back resumes it

---

## Auth and Profile

- [ ] **No auth mode**: UserBadge hidden; `/profile` redirects to `/`
- [ ] **OIDC mode**: UserBadge shows display name; click navigates to `/profile`; profile shows Subject, Email, Groups, Provider; Sign Out button POSTs to `/auth/logout`
- [ ] **Tailscale mode**: UserBadge shows identity; profile shows relevant fields
- [ ] **Cert mode**: profile shows Issuer CN, Certificate Expires, SPIFFE ID
- [ ] **Headers mode**: profile shows identity from trusted headers

---

## Error Handling

- [ ] Navigate to a non-existent route — NotFound page renders with link back to dashboard
- [ ] Navigate to a non-existent resource detail (e.g. `/nodes/invalid`) — FetchError with retry button
- [ ] ErrorBoundary: if a component crashes, inline error state renders with "Try again" button
- [ ] Chart error: if a metrics chart fails to load, retry button appears on the chart
- [ ] `/api/errors` — error index page lists all known error codes with section anchor links
- [ ] `/api/errors/:code` — detail page for a specific error code

---

## Real-Time Updates (SSE)

Verify across multiple pages that SSE events cause live UI updates:

- [ ] **List pages**: deploy a new service → appears in service list without refresh; remove a service → disappears
- [ ] **Detail pages**: scale a service → replica count and tasks update; change node availability → metadata updates
- [ ] **Cluster overview**: any change → health cards and activity feed update
- [ ] **Topology**: add/remove service → layout recomputes after debounce
- [ ] **Stack detail**: scale a service in the stack → task counts update (debounced)
- [ ] **Reconnection**: kill and restart the backend → SSE reconnects; UI recovers
