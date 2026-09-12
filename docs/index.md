---
title: Home
description: Real-time observability and management dashboard for Docker Swarm Mode clusters.
category: overview
tags: [ docker, swarm, dashboard, observability ]
---

# Cetacean

Cetacean is a real-time observability and management dashboard for Docker Swarm Mode clusters. It reads your
cluster through the Docker socket and keeps the page current on its own—there is nothing to poll and nothing to
refresh.

Every resource in your swarm—nodes, services, tasks, stacks, configs, secrets, networks, volumes—is browsable,
cross-referenced and editable in place. The same data is available over a web API and to AI agents over an
embedded MCP server. Add Prometheus and you get CPU and memory charts, capacity bars and sizing
recommendations.

New here? Start with [Getting started][getting-started].

## Documentation

| Page                                          | Covers                                                            |
|-----------------------------------------------|-------------------------------------------------------------------|
| [Getting started][getting-started]            | Install, deploy to a swarm, open the dashboard                    |
| [Configuration][configuration]                | CLI flags, environment variables, TOML file, operations level      |
| [Monitoring][monitoring]                      | Prometheus, node-exporter, and cAdvisor setup                     |
| [Authentication][authentication]              | OIDC, Tailscale, mTLS client certificates, proxy headers          |
| [Authorization][authorization]                | Grant-based RBAC, policy files, provider grant sources            |
| [Dashboard][dashboard]                        | Navigation, command palette, charts, logs, topology               |
| [Integrations][integrations]                  | Traefik, Shepherd, swarm-cronjob, and Diun panels                 |
| [Recommendations][recommendations]            | Automated sizing, config, operational, and cluster checks         |
| [API guide][api]                              | REST endpoints, SSE, feeds, write operations, error codes    |
| [MCP Server][mcp]                             | Enable the MCP server, authorize an agent, prompts and widgets    |
| [MCP tools and resources][mcp-tools]          | Every MCP tool and resource, with its operations level            |

[api]: api
[authentication]: authentication
[authorization]: authorization
[configuration]: configuration
[dashboard]: dashboard
[getting-started]: getting-started
[integrations]: integrations
[mcp]: mcp
[mcp-tools]: mcp-tools
[monitoring]: monitoring
[recommendations]: recommendations
