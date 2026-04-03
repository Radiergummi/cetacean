# JSON Graph Format Topology Design

## Overview

Replace the custom topology serialization format with JSON Graph Format (JGF, https://jsongraphformat.info/) for both the network and placement topology views. The frontend switches to consuming JGF via content negotiation, and a unified `/topology` endpoint serves both graphs in a single multi-graph JGF document.

## Motivation

The current topology endpoints return custom structs that only the built-in frontend understands. JGF is a standard format supported by graph visualization tools (Cytoscape, Gephi, D3) and enables export/interop without custom adapters. The two topology views map naturally to JGF: network topology is a regular undirected graph, placement topology is a hypergraph — both are first-class JGF concepts.

## API Endpoints

### New unified endpoint

`GET /topology`

- `Accept: application/vnd.jgf+json` (or `.jgf` extension suffix) → JGF multi-graph document containing both network and placement graphs
- `Accept: text/html` → SPA
- No `application/json` on this endpoint — JGF is the JSON representation

Response is a JGF document with a `graphs` array:

```json
{
  "graphs": [
    { "id": "network", "type": "network-topology", ... },
    { "id": "placement", "type": "placement-topology", "hyperedges": true, ... }
  ]
}
```

### Deprecated endpoints

`GET /topology/networks` and `GET /topology/placement` remain for backwards compatibility:

- `Accept: application/json` → current custom format (unchanged)
- `Accept: application/vnd.jgf+json` → JGF single-graph document for that view

Both add:
- `Deprecation: true` header (RFC 8594)
- `Link: </topology>; rel="successor-version"` header

These endpoints will be removed in a future major release.

## JGF Schema — Network Topology Graph

```json
{
  "id": "network",
  "type": "network-topology",
  "label": "Network Topology",
  "directed": false,
  "metadata": {
    "@context": "/api/context.jsonld"
  },
  "nodes": {
    "service:svc1": {
      "label": "webapp-api",
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "service",
        "stack": "webapp",
        "replicas": 3,
        "image": "webapp-api:latest",
        "mode": "replicated",
        "ports": ["80:8080/tcp"],
        "updateStatus": "completed",
        "networkAliases": {
          "net1": ["api", "webapp-api"]
        }
      }
    }
  },
  "edges": [
    {
      "source": "service:svc1",
      "target": "service:svc2",
      "metadata": {
        "@context": "/api/context.jsonld",
        "networks": [
          { "id": "net1", "name": "frontend", "driver": "overlay", "scope": "swarm", "stack": "webapp" }
        ]
      }
    }
  ]
}
```

Key decisions:
- Node IDs prefixed with `service:` for global uniqueness across graphs
- `kind` field in metadata distinguishes node types (only `"service"` in network graph)
- Networks embedded in edge metadata — they describe why two services are connected
- Undirected graph; source < target for stable serialization
- Every `metadata` object is an annotated JSON-LD document with `@context`

## JGF Schema — Placement Topology Hypergraph

```json
{
  "id": "placement",
  "type": "placement-topology",
  "label": "Placement Topology",
  "directed": false,
  "hyperedges": true,
  "metadata": {
    "@context": "/api/context.jsonld"
  },
  "nodes": {
    "node:node1": {
      "label": "worker-1",
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "node",
        "role": "worker",
        "state": "ready",
        "availability": "active"
      }
    },
    "service:svc1": {
      "label": "webapp-api",
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "service",
        "mode": "replicated",
        "replicas": 3,
        "image": "webapp-api:latest"
      }
    }
  },
  "hyperedges": [
    {
      "nodes": ["service:svc1", "node:node1", "node:node2"],
      "metadata": {
        "@context": "/api/context.jsonld",
        "tasks": [
          { "id": "task1", "node": "node:node1", "state": "running", "slot": 1, "image": "webapp-api:latest" },
          { "id": "task2", "node": "node:node2", "state": "running", "slot": 2, "image": "webapp-api:latest" }
        ]
      }
    }
  ]
}
```

Key decisions:
- Each service produces one hyperedge connecting it to all nodes where it has tasks
- Service node is first in the `nodes` array by convention
- Tasks are metadata on the hyperedge, not graph nodes
- Each task carries a `node` back-reference for per-node reconstruction
- Service nodes share IDs across both graphs (`service:svc1`), enabling cross-graph correlation
- Cluster nodes prefixed with `node:` to distinguish from services

## Frontend Changes

### API client

New `api.topology()` method fetches `GET /topology` with `Accept: application/vnd.jgf+json`. Returns typed `JGFDocument`.

### Types

JGF type definitions added to `types.ts`:

```typescript
interface JGFDocument {
  graphs: JGFGraph[];
}

interface JGFGraph {
  id: string;
  type: string;
  label: string;
  directed: boolean;
  metadata: JGFMetadata;
  nodes: Record<string, JGFNode>;
  edges?: JGFEdge[];
}

interface JGFHypergraph extends JGFGraph {
  hyperedges: JGFHyperedge[];
}

interface JGFNode {
  label: string;
  metadata: Record<string, unknown> & { "@context": string };
}

interface JGFEdge {
  source: string;
  target: string;
  metadata: Record<string, unknown> & { "@context": string };
}

interface JGFHyperedge {
  nodes: string[];
  metadata: Record<string, unknown> & { "@context": string };
}
```

### Transform layer

Replace `buildLogicalFlow` and `buildPhysicalFlow` with:

- `networkGraphToReactFlow(graph: JGFGraph)` — extracts service nodes (by `kind === "service"`), builds ReactFlow nodes with stack grouping, creates edges from `graph.edges` with network metadata. Same ELK layout pipeline.
- `placementGraphToReactFlow(graph: JGFGraph)` — extracts node and service entries, reconstructs per-node task lists from hyperedge metadata, builds the physical grid layout. Same positioning logic.

### Topology page

Fetches once via `api.topology()`, picks graphs by `id` (`"network"` / `"placement"`), passes each to its view-specific transform. Old `api.topologyNetworks()` and `api.topologyPlacement()` calls removed.

Old types (`NetworkTopology`, `PlacementTopology`, `TopoServiceNode`, etc.) removed once deprecated endpoints are removed.

## ACL, Caching, and What Doesn't Change

- **ACL:** Same filtering as today. Services filtered by `acl.Filter`, nodes filtered by `acl.Filter`, tasks filtered to readable services. Filtering applied before JGF serialization. Hyperedges for inaccessible services not emitted.
- **ETag/304:** JGF responses use `writeCachedJSON` — SHA-256 ETag, conditional 304.
- **SSE:** No SSE on topology (same as today — fetch-on-demand).
- **Content negotiation on `/topology`:** Only `application/vnd.jgf+json` and `text/html`. No `application/json`.

## JSON-LD Context

Extend `/api/context.jsonld` with topology vocabulary terms: `kind`, `replicas`, `mode`, `role`, `state`, `availability`, `ports`, `networkAliases`, `tasks`, `slot`, `image`, `stack`, `updateStatus`, `driver`, `scope`. Simple term definitions — no external ontology.

## OpenAPI

Add unified `/topology` endpoint with `application/vnd.jgf+json` media type. Mark `/topology/networks` and `/topology/placement` as deprecated.
