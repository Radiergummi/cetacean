---
title: Getting Started
description: Install Cetacean, deploy to a Docker Swarm cluster, and add monitoring.
category: guide
tags: [installation, docker, swarm, quickstart]
---

# Getting Started

## Requirements

- A Docker Swarm Mode cluster (single-node swarms work fine)
- Access to a manager node's Docker socket

## Installation

### Docker Swarm (recommended)

Deploy Cetacean as a stack service. It needs to run on a manager node for Docker API access:

```yaml
services:
  cetacean:
    image: cetacean:latest
    ports:
      - "9000:9000"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    deploy:
      placement:
        constraints:
          - node.role == manager
```

```bash
docker stack deploy -c compose.yaml cetacean
```

Cetacean syncs the full swarm state on startup (typically under a second). The built-in `HEALTHCHECK` gates on this
sync, so downstream services can use `depends_on: { cetacean: { condition: service_healthy } }`.

Open [http://localhost:9000](http://localhost:9000).

### From source

```bash
cd frontend && npm install && npm run build && cd ..
go build -o cetacean .
./cetacean
```

### Pre-built binary

```bash
./cetacean  # uses /var/run/docker.sock by default
```

## Adding Monitoring

Cetacean works without Prometheus, but metrics unlock CPU/memory charts, resource gauges, and capacity bars.

Deploy the bundled monitoring stack (Prometheus + node-exporter + cAdvisor) and point Cetacean at it:

```bash
docker stack deploy -c compose.monitoring.yaml monitoring
```

```yaml
environment:
  CETACEAN_PROMETHEUS_URL: http://prometheus:9090
```

Both stacks need to share an overlay network. See [Monitoring](monitoring.md) for the full setup.

## Adding Authentication

By default, anyone who can reach Cetacean has full access. To restrict access, configure an auth provider — OIDC,
Tailscale, mTLS certificates, or trusted proxy headers. See [Authentication](authentication.md).

Once authenticated, you can optionally add per-resource access control with [Authorization](authorization.md).

## Subscribing to Changes

Every resource page provides an Atom feed — click the feed icon in the page header or append `.atom` to any resource
URL. Subscribe in your feed reader to get notified when services, nodes, or other resources change.

## Configuration

See [Configuration](configuration.md) for all settings. The highlights:

- `operations_level` controls which write operations are available (default `1` = safe ops like scale and restart; `0`
  for read-only)
- `base_path` for sub-path deployments behind a reverse proxy
- `snapshot` persists swarm state to disk for fast restarts
