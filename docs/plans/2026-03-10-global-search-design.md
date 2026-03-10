# Global Search Design

## Overview

Add a global search feature to Cetacean that searches across all resource types (services, stacks, nodes, tasks, configs, secrets, networks, volumes) from a single input. Results appear in a command-palette-style dropdown for quick navigation, with a dedicated full-page view for deeper exploration.

## Backend

### New endpoint: `GET /api/search?q=<term>`

Single endpoint that searches all resource types and returns grouped results. Case-insensitive substring matching (consistent with existing `searchFilter`).

#### Search fields per resource type

| Type | Fields searched |
|------|----------------|
| Services | `Spec.Name`, `Spec.TaskTemplate.ContainerSpec.Image`, label keys/values |
| Nodes | `Description.Hostname`, `Status.Addr`, label keys/values |
| Stacks | `Name` |
| Tasks | service name (via cache cross-reference), image, label keys/values |
| Configs | `Spec.Name`, label keys/values |
| Secrets | `Spec.Name`, label keys/values |
| Networks | `Name`, label keys/values |
| Volumes | `Name`, label keys/values |

#### Response shape

```json
{
  "query": "nginx",
  "results": {
    "services": [
      { "id": "abc123", "name": "nginx-web", "detail": "nginx:1.25-alpine" }
    ],
    "stacks": [
      { "id": "nginx-ingress", "name": "nginx-ingress", "detail": "3 services" }
    ]
  },
  "counts": {
    "services": 2, "stacks": 1, "nodes": 0, "tasks": 0,
    "configs": 1, "secrets": 0, "networks": 1, "volumes": 0
  },
  "total": 5
}
```

- `results`: Max 3 items per type. Types with 0 matches are omitted from this map.
- `counts`: Always includes all 8 types (for the "View all results" page to show totals).
- `total`: Sum of all counts.
- Each result item has `id` (or `name` for stacks/volumes), `name` (display label), and `detail` (secondary info).

#### Detail field per type

| Type | Detail content |
|------|---------------|
| Services | Image name |
| Nodes | Role + state (e.g., "manager, ready") |
| Stacks | Service count |
| Tasks | State + service name |
| Configs | Created date |
| Secrets | Created date |
| Networks | Driver |
| Volumes | Driver |

#### Type ordering in response

Services > Stacks > Nodes > Tasks > Configs > Secrets > Networks > Volumes

#### Implementation

- New handler `HandleSearch` in `internal/api/handlers.go`
- Route: `GET /api/search` in `internal/api/router.go`
- Requires `?q=` param with minimum 1 character; returns 400 if empty
- Searches all cached resources, builds result groups, caps at 3 per type
- Label search: iterate `Spec.Labels` (or `Labels` for networks/volumes), match against both key and value

### No new cache methods needed

The handler reads from existing `List*` methods on the cache and applies search logic inline. No new indexes or data structures.

## Frontend

### Components

#### 1. `GlobalSearch` (nav bar trigger)

Location: Right side of nav bar, between nav links and theme toggle.

Compact button containing:
- Magnifying glass icon
- "Search..." placeholder text
- `Cmd+K` keyboard shortcut badge

Clicking the button or pressing `Cmd+K` opens the `SearchPalette`.

On mobile: renders as just the magnifying glass icon button in the header.

#### 2. `SearchPalette` (overlay modal)

Centered modal with backdrop blur, similar to GitHub's command palette or VS Code's Cmd+P.

Structure:
- **Search input**: Auto-focused, debounced 300ms, calls `GET /api/search?q=...`
- **Results area**: Grouped by type with uppercase section headers. Each row shows resource name and detail text. First result highlighted by default.
- **Footer**: Result count on left, "View all results →" link on right.

Behavior:
- Arrow Up/Down to navigate results
- Enter to navigate to the highlighted resource's detail page
- Escape to close
- Click on backdrop to close
- Closes after navigation
- Empty query shows no results
- No matches shows "No results for 'xyz'"

#### 3. `SearchPage` (`/search?q=...`)

Full-page search results view. Route: `/search`.

- Pre-filled search input at top
- All results shown (no 3-per-type cap) — uses a separate backend call or query param to lift the limit
- Same grouped-by-type layout with section headers
- Each row links to the resource detail page
- Empty sections hidden
- URL-persisted query via `?q=` param

### API client addition

```typescript
search: (q: string): Promise<SearchResponse> =>
  fetchJSON(`/api/search?q=${encodeURIComponent(q)}`)
```

### New types

```typescript
interface SearchResult {
  id: string;
  name: string;
  detail: string;
}

interface SearchResponse {
  query: string;
  results: Partial<Record<ResourceType, SearchResult[]>>;
  counts: Record<ResourceType, number>;
  total: number;
}

type ResourceType = "services" | "stacks" | "nodes" | "tasks" | "configs" | "secrets" | "networks" | "volumes";
```

### Keyboard shortcut

Register a global `keydown` listener for `Cmd+K` (Mac) / `Ctrl+K` (other) that opens the palette. Prevent default browser behavior (Cmd+K typically focuses the browser address bar).

## Full page variant

The `/api/search` endpoint accepts an optional `?limit=0` parameter to return all results instead of the default 3-per-type cap. The full search page uses this.

## What this does NOT include

- Fuzzy matching or typo tolerance — substring is sufficient for the data sizes involved
- Search ranking/scoring — results within a type are unordered (same as existing list search)
- Search history or recent searches
- Type-ahead suggestions before typing
- WebSocket/SSE live-updating search results
