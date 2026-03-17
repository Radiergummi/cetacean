# Changelog

All notable changes to Cetacean will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Last deployment diff on service detail page showing what changed in the most recent service update

### Fixed
- Log viewer appearing empty on service and task detail pages until user interaction

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
