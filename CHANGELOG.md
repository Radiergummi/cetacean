# Changelog

All notable changes to Cetacean will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Label-based access control — teams can add `cetacean.acl.read` and `cetacean.acl.write` labels to Docker resources to control who can view and modify them, without a central policy file; view and edit ACL labels on the service detail page
- Atom feed support on resource endpoints — request via `Accept: application/atom+xml` header or `.atom` URL suffix
- Feed icon button in page headers for pages with Atom feeds
- `Link: rel="alternate"` header on JSON responses advertising the Atom feed URL

## [0.11.0] - 2026-04-02

### Added
- General trusted proxies setting (`CETACEAN_TRUSTED_PROXIES`) for real client IP resolution behind reverse proxies — replaces the headers-auth-specific setting, which is now deprecated
- Client IP in structured request logs when trusted proxies are configured
- CLI flags for all settings that were previously env-var-only: `-operations-level`, `-sse-batch-interval`, `-cors-origins`, `-snapshot`, `-data-dir`, `-trusted-proxies`
- Gzip compression for snapshot files (existing plain JSON snapshots are read transparently)
- Tailscale auth mode comparison table in the authentication docs
- Configurable CORS support for cross-origin API access (`CETACEAN_CORS_ORIGINS`)
- Grant-based RBAC authorization with per-resource access control
- `Allow` response header indicating available methods per resource — the dashboard uses this to show or hide write controls per resource and per user
- `Accept-Patch` response header advertising supported patch formats per resource (RFC 5789)
- `Prefer: return=minimal` support on all write endpoints (RFC 7240)
- `Last-Modified` / `If-Modified-Since` conditional requests on resource detail endpoints
- `Strict-Transport-Security` (HSTS) header when TLS is enabled
- Structured error responses (RFC 9457) for all authentication and OIDC callback errors
- SSE keepalive comments on idle connections to prevent proxy timeouts
- Footer with version info, GitHub, docs, and API links
- Table/grid view toggle on the tasks page with tasks grouped by service in card view
- Arrow-key navigation for radio card groups
- Series labels in the Prometheus query result table
- Per-stack CPU and memory usage charts on the node detail page with drill-down to individual services
- HTTP Range Request pagination on all list API endpoints (`Range: items 0-49` returns `206 Partial Content` with `Content-Range`)
- Infinite scroll on all resource list pages — items load automatically as you scroll down

### Deprecated
- `CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES` — use `CETACEAN_TRUSTED_PROXIES` instead; will be removed in v1

### Security
- Service tasks, service logs, task logs, and node tasks endpoints now enforce ACL read checks — previously accessible to any authenticated user regardless of grants
- Topology endpoints now filter services and nodes by per-resource ACL grants instead of showing the entire cluster to any authenticated user
- History endpoint now filters events by per-resource ACL read permission instead of showing all resource changes
- SSE event replay on reconnect now applies ACL filtering (previously replayed events bypassed ACL)
- Policy validation rejects malformed glob patterns at load time instead of silently creating dead grants
- Monitoring status, Prometheus label names, and label values endpoints moved from unauthenticated `/-/` prefix to authenticated `/metrics/` — previously exposed cluster node count and Prometheus label data without authentication
- Task-to-service-to-stack ACL inheritance chain now resolves correctly (previously stopped at service level)

### Changed
- `X-Request-ID` header renamed to `Request-Id` per RFC 6648 (deprecation of `X-` prefix)

### Fixed
- Layout shift in the header when the live connection timer changes width
- Pressing Escape in a combobox dropdown closing the parent editor panel
- Node metrics table columns, gauges, and chart tooltips when the Prometheus instance label uses a hostname instead of an IP
- Sizing recommendations comparing aggregate usage across all tasks to per-task limits, producing incorrect percentages and suggestions for multi-replica services

## [0.10.0] - 2026-03-31

### Added
- Self-metrics endpoint (`/-/metrics`) documented in monitoring guide with full metric reference
- `CETACEAN_SELF_METRICS` toggle to disable the self-metrics endpoint (enabled by default)
- `CETACEAN_RECOMMENDATIONS` toggle to disable the recommendation engine (enabled by default)
- Task state filter on the tasks page — filter by running, failed, or any other state via segmented control
- "Failed Tasks" card on the cluster overview now links directly to failed tasks
- Recommendation cards expand to show why each recommendation matters
- Monitoring status banner shows the actual error when Prometheus is unreachable

### Fixed
- Relative timestamps ("5 minutes ago") never updating while the page stays open
- Chart data briefly corrupted when switching time ranges on metrics panels
- Task state not updating in real time when replicas finish starting, requiring a 5-minute wait for the periodic re-sync
- Prometheus proxy returning raw 404/502 responses instead of structured errors when the configured URL is wrong
- Prometheus query client ignoring HTTP error status codes, producing misleading "parse error" messages
- Base path detection using page URL instead of `<base>` tag, causing broken API requests when navigating in the dev server
- API requests hanging indefinitely when Prometheus or Docker is unresponsive (now timeout after 30 seconds)
- Empty state messages not announced by screen readers

## [0.9.1] - 2026-03-30

### Fixed
- Assets not loading when deployed under a base path

## [0.9.0] - 2026-03-30

### Added
- Sub-path deployment: serve Cetacean under a configurable URL prefix (e.g., `/cetacean/`) via `CETACEAN_BASE_PATH` environment variable, `--base-path` flag, or `base_path` TOML config
- Recommendation engine: `/recommendations` page with cluster health checks across resource sizing, config hygiene, operational health, and cluster topology — with one-click fixes for auto-fixable items
- Integration detection: Traefik, Shepherd, Swarm Cronjob, and Diun labels shown as structured panels on service detail pages, with inline editing

## [0.8.2] - 2026-03-27

### Fixed
- Release Docker image running as unprivileged user, preventing Docker socket access
- Release Docker image healthcheck not detecting Docker connectivity issues
- Minimal scratch base image for release builds (was alpine)

## [0.8.1] - 2026-03-27

### Fixed
- Resource pages showing empty states instead of errors when Docker socket is unreachable
- Container running as unprivileged user unable to access Docker socket
- Watcher reconnect loop spamming API with sync events on persistent Docker failures

## [0.8.0] - 2026-03-26

### Added
- Label editing for configs and secrets
- Config and secret creation from the dashboard
- Plugin management: browsable list and detail pages, install/upgrade with privilege review, enable/disable/remove/configure
- Swarm configuration editors: per-section editing for raft, CA, orchestration, and task defaults, with token rotation and unlock key retrieval
- Structured error codes across all API error responses with browsable error reference at `/api/errors`
- Toast notifications for mutation errors with contextual suggestions from the error code registry
- Config, secret, network, and volume removal from detail pages with confirmation dialogs
- Node role change (promote/demote) with radio card selector and quorum impact warnings
- Node removal with type-to-confirm hostname dialog (only available for down nodes)
- Stack removal with type-to-confirm dialog (removes all services, configs, secrets, and networks)
- Service mounts editor with card-based UI supporting all Docker mount types (bind, volume, tmpfs, npipe, cluster, image)
- Service config, secret, and network attachment editors (add/remove references with searchable dropdowns)
- Container configuration editors: command, runtime options, capabilities, extra hosts, DNS settings
- Browsable pages for service sub-resource endpoints (env, placement, ports, policies, log driver, mounts, attachments)
- Docker documentation help links on all service editor fields
- Deployment diff and recent activity shown side by side on wide screens

### Improved
- Detail pages use SSE event payloads for instant sub-resource updates instead of refetching (10 → 2 HTTP requests per event on service pages)
- Healthcheck timeline is keyboard-accessible and scrollable on small screens
- Combobox dropdowns show resource IDs as subtitles for disambiguation
- Editor sections show configured values only, with empty states and edit hints

### Fixed
- Config and secret editors auto-filling incorrect target paths for stack-namespaced resources
- Escape key in combobox dropdowns closing the parent editor instead of just the dropdown

## [0.7.0] - 2026-03-21

### Added
- Operations level setting to restrict write operations by danger tier
- Scale, update image, rollback, and restart actions on the service detail page
- Drain, activate, and pause node availability controls on the node detail page
- Force Remove button on task detail page to kill a task and its backing container
- Last deployment diff on service detail page showing what changed in the most recent service update
- Inline environment variable editor on service detail page (add, edit, remove variables)
- Inline node label editor on node detail page (add, edit, remove labels)
- Inline service label editor on service detail page
- Service resource limits editor on service detail page (CPU and memory limits/reservations)
- Service mode switching (replicated/global) and endpoint mode editing (VIP/DNS-RR)
- Write actions accessible via the command palette (Cmd+K)
- Cluster capacity API endpoint for resource slider bounds
- PromQL metrics query console with autocompletion for metric names, functions, and label values
- Segment-prefix fuzzy matching in global search and PromQL autocompletion
- Copy-to-clipboard buttons on container configuration and healthcheck values
- Docker Swarm template expressions (e.g. `{{.Node.Hostname}}`) rendered as human-readable badges
- Resizable log viewer (drag bottom edge, double-click to reset)
- Fullscreen log viewer via browser Fullscreen API
- Service placement, ports, update/rollback policy, and log driver read and write endpoints
- Healthcheck editor on service detail page (view and edit healthcheck configuration)
- Interactive healthcheck timeline on task detail page
- Delete service button on service detail page
- Config file auto-discovery from standard locations
- Mobile-responsive layout (navigation, topology, log viewer, search palette, charts)
- Replica doughnut chart on service detail page

### Improved
- All mutation forms use polished shadcn/ui components (popovers, confirmation dialogs, styled dropdowns)
- Resource limits editor shows sliders with cluster-aware bounds and accepts memory in megabytes
- Scale replica input has increment/decrement buttons
- Destructive actions use styled confirmation dialogs instead of browser-native confirms
- Native title attributes replaced with proper tooltips across service detail page
- Activity feed on detail pages no longer shows redundant resource type badge
- Activity feed uses stack-prefix rendering for resource names

### Fixed
- Log viewer appearing empty on service and task detail pages until user interaction
- Node metrics showing when node-exporter targets are not available
- Duplicate labels section on service detail page
- Long values overflowing in last deployment section
- Command palette showing write actions above the configured operations level

## [0.6.0] - 2026-03-16

### Added
- Pluggable authentication with five providers: anonymous, OIDC, Tailscale, mTLS client certificates, and trusted proxy headers
- Identity display in the navigation bar when authentication is enabled
- `_FILE` suffix support for secret environment variables (for Docker Swarm secrets)

### Security
- Session cookies use `__Host-` prefix for browser-enforced security constraints

## [0.5.0] - 2026-03-16

### Added
- Tasks list page (the "Failed Tasks" card on the homepage no longer 404s)
- Click-to-isolate on chart legend items in stack drill-down charts
- Keyboard shortcut tooltips on navigation links (hover to discover shortcuts)

### Changed
- Lazy-load all pages and split heavy vendor libraries (Chart.js, topology) into separate chunks — initial load reduced from 2.3 MB to ~360 KB

### Fixed
- Log viewer "has more" indicator inaccurate when filtering by stream (stdout/stderr)
- List page item counts not updating on real-time add/remove events
- Race condition when multiple metrics streams connect simultaneously
- Stale fetch requests not cancelled when navigating away from detail/search pages
- Ghost stacks appearing after all services removed but configs/secrets remain
- Metrics charts not reconnecting SSE stream after tab becomes visible again

## [0.4.0] - 2026-03-16

### Added
- Live-updating charts via SSE streaming (no more manual refresh for recent time ranges)
- Range picker with quick presets and custom date-time selection

### Changed
- Replace auto-refresh toggle with streaming play/pause control

### Fixed
- Click-to-isolate and double-click drill-down racing on chart interactions
- Doughnut chart sizing on homepage

## [0.3.0] - 2026-03-16

### Added
- TOML config file and CLI flags as alternative to environment variables
- `healthcheck` subcommand for container health checks

### Fixed
- Simplify disk usage chart to single ring (two-ring layout was confusing; reclaimable info moved to tooltip)

## [0.2.0] - 2026-03-15

### Added
- Migrate from uPlot to Chart.js for all charting
- CVD-safe color palette with theme integration
- Interactive charts: linked crosshairs, click-to-isolate, brush-to-zoom
- Custom date-time range picker with quick presets
- Stacked area toggle on time series charts
- Stack-based drill-down on cluster overview
- Resource allocation bar chart on service detail
- Mock chart data in dev mode when Prometheus is unavailable

### Fixed
- Search input no longer loses focus on refetch
- List pages no longer flash loading skeleton on search/sort

## [0.1.11] - 2026-03-14

### Fixed
- Log fetches for multi-replica services reduced from ~10s to ~2s (idle timeout on stream parser)

## [0.1.10] - 2026-03-14

### Fixed
- Frontend test failures after monitoring status hook was added

## [0.1.9] - 2026-03-14

### Fixed
- Stale browser-cached responses (added Cache-Control headers)
- Data race in cross-reference lookups under concurrent reads
- Dockerfile healthcheck path
- Docker Compose external network name for monitoring stack

### Security
- Bump undici

## [0.1.8] - 2026-03-14

### Added
- Per-task CPU/memory sparklines on service, node, and task pages
- Metrics panels on cluster overview, node list, and service list
- Leader badge, availability, and address columns on node table
- Ports column on service table

### Fixed
- Resource gauges crash when no resource limit is set
- Page titles all showed "frontend" instead of the resource name
- Tables clip on small viewports

## [0.1.7] - 2026-03-13

### Fixed
- Double borders on deploy config detail panels
- Ghost stacks appearing from orphaned volumes

## [0.1.6] - 2026-03-13

### Fixed
- Duplicate edges in logical topology view
- Overlapping nodes in physical topology view
- Stack health incorrectly counting historical task failures

## [0.1.5] - 2026-03-13

### Fixed
- Node metrics showing wrong values in overlay network deployments (now resolves by hostname instead of IP)

## [0.1.4] - 2026-03-13

### Fixed
- Service replica counts inflated by shutdown tasks
- Log viewer making unnecessary polling requests
- Spurious task change events in activity feed
- Running tasks not sorting first in task lists
- Task detail service link not clickable

## [0.1.3] - 2026-03-13

### Fixed
- Dockerfile naming and base image updates for release builds

## [0.1.2] - 2026-03-13

### Fixed
- SSE test data race under `-race` flag
- CI compatibility with Go 1.26 and latest GitHub Actions

### Security
- Bump hono to 4.12.7 (prototype pollution CVE)

## [0.1.1] - 2026-03-13

### Fixed
- CI and dependency fixes

## [0.1.0] - 2026-03-12

### Added
- Per-resource SSE streaming on all list and detail endpoints
- OpenAPI 3.1 spec with Scalar API playground at `/api`
- JSON-LD metadata and RFC 9457 error responses
- Content negotiation via `Accept` header or `.json`/`.html` extension
- ETag conditional caching with 304 Not Modified
- Global cross-resource search with `Cmd+K` command palette
- Network topology view (logical and physical)
- Stack detail pages with member resources
- Log viewer with live streaming, regex search, JSON formatting
- Monitoring auto-detection (Prometheus, cAdvisor, node-exporter)
- Node resource gauges and service/stack metrics panels
- Disk snapshot persistence for instant dashboard on restart
- Expression-based filtering on all list endpoints
- Virtual scrolling for large tables
- Activity feed with recent resource changes
- Multi-platform Docker images (amd64, arm64) with SBOM and provenance

### Security
- Secret values never exposed in API responses
- Prometheus proxy restricted to query endpoints
- Connection limits: 256 SSE clients, 128 concurrent log streams
