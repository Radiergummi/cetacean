---
title: Home
description: Real-time observability and management dashboard for Docker Swarm Mode clusters.
category: overview
tags: [ docker, swarm, dashboard, observability ]
---

# Cetacean

Cetacean is a real-time observability and management dashboard for Docker Swarm Mode clusters. It connects to the
Docker socket, caches swarm state in memory, and pushes updates to browsers via SSE — no polling, no refresh.

Every resource in your swarm (nodes, services, tasks, configs, secrets, networks, volumes) is browsable with
cross-references, live metrics, and inline editing. Optional Prometheus integration adds CPU/memory charts, capacity
bars, and resource sizing recommendations.

## Documentation

|                                    |                                                |
|------------------------------------|------------------------------------------------|
| [Getting Started](getting-started) | Install, deploy, and add monitoring            |
| [Configuration](configuration)     | Flags, env vars, TOML config, operations level |
| [Monitoring](monitoring)           | Prometheus, node-exporter, cAdvisor            |
| [Authentication](authentication)   | OIDC, Tailscale, mTLS, proxy headers           |
| [Authorization](authorization)     | Grant-based RBAC per resource                  |
| [Dashboard Guide](dashboard)       | Shortcuts, command palette, charts, logs       |
| [Integrations](integrations)       | Traefik, Shepherd, Swarm Cronjob, Diun         |
| [Recommendations](recommendations) | Automated cluster health checks                |
| [API Reference](api)               | REST, SSE, write operations, error codes       |
