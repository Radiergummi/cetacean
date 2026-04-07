# JSON Graph Format Topology Design

## Overview

Replace the custom topology serialization format with JSON Graph Format (JGF, https://jsongraphformat.info/) for both the network and placement topology views. The frontend switches to consuming JGF via content negotiation, and a unified `/topology` endpoint serves both graphs in a single multi-graph JGF document.

## Motivation

The current topology endpoints return custom structs that only the built-in frontend understands. JGF is a standard format supported by graph visualization tools (Cytoscape, Gephi, D3) and enables export/interop without custom adapters. Both topology views map naturally to JGF hypergraphs: edges represent pairwise relationships (network connectivity), hyperedges represent group relationships (stack membership, service placement).

## API Endpoints

### New unified endpoint

`GET /topology`

- `Accept: application/vnd.jgf+json` (or `.jgf` extension suffix) → JGF multi-graph document containing both network and placement graphs
- `Accept: text/html` → SPA
- No `application/json` on this endpoint — JGF is the JSON representation

Response is a JGF document with a `graphs` array (both are hypergraphs):

```json
{
  "graphs": [
    { "id": "network", "type": "network-topology", ... },
    { "id": "placement", "type": "placement-topology", ... }
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

## JGF Schema — Network Topology Hypergraph

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
    "urn:cetacean:service:svc1": {
      "label": "webapp-api",
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "service",
        "replicas": 3,
        "image": "webapp-api:latest",
        "mode": "replicated",
        "ports": ["80:8080/tcp"],
        "updateStatus": "completed"
      }
    },
    "urn:cetacean:service:svc2": {
      "label": "webapp-web",
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "service",
        "replicas": 2,
        "image": "webapp-web:latest",
        "mode": "replicated"
      }
    },
    "urn:cetacean:service:svc3": {
      "label": "prometheus",
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "service",
        "replicas": 1,
        "image": "prom/prometheus:latest",
        "mode": "replicated"
      }
    }
  },
  "edges": [
    {
      "source": "urn:cetacean:service:svc1",
      "target": "urn:cetacean:service:svc2",
      "metadata": {
        "@context": "/api/context.jsonld",
        "networks": [
          {
            "id": "urn:cetacean:network:net1",
            "name": "frontend",
            "driver": "overlay",
            "scope": "swarm",
            "aliases": {
              "urn:cetacean:service:svc1": ["api"],
              "urn:cetacean:service:svc2": ["web"]
            }
          }
        ]
      }
    }
  ],
  "hyperedges": [
    {
      "nodes": ["urn:cetacean:service:svc1", "urn:cetacean:service:svc2"],
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "stack",
        "name": "webapp"
      }
    },
    {
      "nodes": ["urn:cetacean:service:svc3"],
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "stack",
        "name": "monitoring"
      }
    }
  ]
}
```

Key decisions:
- All entity IDs use `urn:cetacean:<type>:<id>` URNs (e.g., `urn:cetacean:service:svc1`, `urn:cetacean:node:node1`, `urn:cetacean:network:net1`) for globally unique, unambiguous identifiers
- `kind` field in metadata distinguishes entity types (`"service"` for nodes, `"stack"` for hyperedges)
- **Stack membership is a hyperedge**, not denormalized node metadata — structurally represents the group relationship
- **Network aliases are edge metadata**, keyed by endpoint URN — aliases are properties of a service's attachment to a network, not of the service itself
- Networks embedded in edge metadata — they describe pairwise connectivity
- **Edges** = pairwise relationships (network connectivity between services)
- **Hyperedges** = group relationships (stack membership)
- Undirected graph; source < target on edges for stable serialization
- Every `metadata` object is an annotated JSON-LD document with `@context`
- Services not belonging to any stack have no stack hyperedge

## JGF Schema — Placement Topology Hypergraph

Both graphs are hypergraphs. The conceptual model is uniform: edges are pairwise relationships, hyperedges are group relationships.

```json
{
  "id": "placement",
  "type": "placement-topology",
  "label": "Placement Topology",
  "directed": false,
  "metadata": {
    "@context": "/api/context.jsonld"
  },
  "nodes": {
    "urn:cetacean:node:node1": {
      "label": "worker-1",
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "node",
        "role": "worker",
        "state": "ready",
        "availability": "active"
      }
    },
    "urn:cetacean:node:node2": {
      "label": "worker-2",
      "metadata": {
        "@context": "/api/context.jsonld",
        "kind": "node",
        "role": "manager",
        "state": "ready",
        "availability": "active"
      }
    },
    "urn:cetacean:service:svc1": {
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
      "nodes": ["urn:cetacean:service:svc1", "urn:cetacean:node:node1", "urn:cetacean:node:node2"],
      "metadata": {
        "@context": "/api/context.jsonld",
        "tasks": [
          { "id": "urn:cetacean:task:task1", "node": "urn:cetacean:node:node1", "state": "running", "slot": 1, "image": "webapp-api:latest" },
          { "id": "urn:cetacean:task:task2", "node": "urn:cetacean:node:node2", "state": "running", "slot": 2, "image": "webapp-api:latest" }
        ]
      }
    }
  ]
}
```

Key decisions:
- All entity IDs use `urn:cetacean:<type>:<id>` URNs, consistent with the network graph
- Each service produces one hyperedge connecting it to all nodes where it has tasks
- Service URN is first in the `nodes` array by convention
- Tasks are metadata on the hyperedge, not graph nodes
- Each task carries a `node` URN back-reference for per-node reconstruction
- Task IDs are also URNs (`urn:cetacean:task:<id>`)
- Service nodes share URNs across both graphs (`urn:cetacean:service:svc1`), enabling cross-graph correlation

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
  hyperedges?: JGFHyperedge[];
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

Both graphs use `edges` and/or `hyperedges` as needed. The network graph has both (edges for network connectivity, hyperedges for stack membership). The placement graph has only hyperedges (service-to-nodes placement).

### Transform layer

Replace `buildLogicalFlow` and `buildPhysicalFlow` with:

- `networkGraphToReactFlow(graph: JGFGraph)` — extracts service nodes (by `kind === "service"`), extracts stack groups from hyperedges (by `kind === "stack"`), builds ReactFlow group nodes for stacks and child nodes for services, creates edges from `graph.edges` with network metadata. Same ELK layout pipeline.
- `placementGraphToReactFlow(graph: JGFGraph)` — extracts cluster node and service entries from `graph.nodes`, reconstructs per-node task lists from hyperedge metadata, builds the physical grid layout. Same positioning logic.

### Topology page

Fetches once via `api.topology()`, picks graphs by `id` (`"network"` / `"placement"`), passes each to its view-specific transform. Old `api.topologyNetworks()` and `api.topologyPlacement()` calls removed.

Old types (`NetworkTopology`, `PlacementTopology`, `TopoServiceNode`, etc.) removed once deprecated endpoints are removed.

## ACL, Caching, and What Doesn't Change

- **ACL:** Same filtering as today. Services filtered by `acl.Filter`, nodes filtered by `acl.Filter`, tasks filtered to readable services. Filtering applied before JGF serialization. Hyperedges for inaccessible services not emitted.
- **ETag/304:** JGF responses use `writeCachedJSON` — SHA-256 ETag, conditional 304.
- **SSE:** No SSE on topology (same as today — fetch-on-demand).
- **Content negotiation on `/topology`:** Only `application/vnd.jgf+json` and `text/html`. No `application/json`.

## JSON-LD Context

Extend `/api/context.jsonld` with topology vocabulary terms: `kind`, `name`, `replicas`, `mode`, `role`, `state`, `availability`, `ports`, `aliases`, `tasks`, `slot`, `image`, `updateStatus`, `driver`, `scope`, `networks`. Simple term definitions — no external ontology.

## URN Scheme

All entity identifiers use the `urn:cetacean:<type>:<id>` format:

| Entity | URN pattern | Example |
|--------|-------------|---------|
| Service | `urn:cetacean:service:<docker-id>` | `urn:cetacean:service:abc123` |
| Node | `urn:cetacean:node:<docker-id>` | `urn:cetacean:node:def456` |
| Task | `urn:cetacean:task:<docker-id>` | `urn:cetacean:task:ghi789` |
| Network | `urn:cetacean:network:<docker-id>` | `urn:cetacean:network:jkl012` |

URNs are used as JGF node keys, edge source/target references, hyperedge node references, and task/network `id` fields within metadata. The `<id>` component is the Docker resource ID (or name for volumes). The URN scheme provides globally unique, type-safe identifiers that are consistent across both graphs and enable cross-graph correlation.

## OpenAPI

Add unified `/topology` endpoint with `application/vnd.jgf+json` media type. Mark `/topology/networks` and `/topology/placement` as deprecated.
