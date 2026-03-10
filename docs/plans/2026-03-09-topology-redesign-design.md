# Topology Page Redesign

## Problem

The current topology page uses a D3 force-directed graph that is hard to read and doesn't convey useful information at scale. We want a card-based layout that shows service relationships clearly.

## Approach

React Flow + dagre for auto-layout. Two views toggled by a segmented control.

## Views

### Logical View (grouped by stack)

- **StackGroup containers**: one per stack, labeled with stack name
- **ServiceCard nodes** inside their stack group (or ungrouped if no stack label)
- **Network edges**: colored per overlay network, smoothstep routing
  - Hover shows tooltip: network name, driver, scope
  - NetworkLegend panel maps colors to network names; click to highlight
- Layout: dagre LR with compound nodes

### Physical View (grouped by node)

- **NodeGroup containers**: one per swarm node
  - Header: hostname + role badge (Manager/Worker), state dot, availability
- **TaskCard nodes** inside their node group
  - Shows: service name + slot (e.g. `nginx.3`), state dot, image tag
  - Hover highlights all tasks of the same service across all nodes
- No edges

## Card Content

### ServiceCard (logical view)
- Service name (truncated with tooltip)
- Mode badge: Replicated / Global
- Image tag (e.g. `nginx:1.25`, stripped of registry prefix)
- Replicas: `3/3` with status dot (green/yellow/red)
- Published ports (e.g. `80->8080`)
- Update status (if actively updating)

### TaskCard (physical view)
- Service name + slot number
- State with colored dot
- Image tag

## API Changes

Enrich existing topology endpoints with additional fields:

**`TopoServiceNode`** — add:
- `Image string`
- `Ports []string`
- `Mode string`
- `UpdateStatus string`

**`TopoTask`** — add:
- `Image string`

Data is already available in cache from Docker service/task objects.

## Dependencies

**Add:** `@xyflow/react`, `dagre`
**Remove:** `d3-force`, `d3-drag` (and `d3-selection`, `d3-zoom` if unused elsewhere)

## Layout

- Dagre LR direction for both views
- Compound node support for stack/node groups
- Layout recomputed on data change (SSE triggers debounced refetch)
