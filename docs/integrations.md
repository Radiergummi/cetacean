---
title: Integrations
description: Structured panels for Traefik, Shepherd, Swarm Cronjob, and Diun on service detail pages.
category: guide
tags: [integrations, traefik, shepherd, swarm-cronjob, diun]
---

# Integrations

Cetacean detects well-known Docker Swarm ecosystem tools from service labels and renders them as structured panels on
the service detail page. Panels appear above the labels section; if no recognized labels are present, nothing is shown.

## Supported Tools

| Tool | Labels | Description |
|---|---|---|
| [Traefik](https://traefik.io/) | `traefik.*` | HTTP routers, services, and middlewares parsed from `traefik.http.*` labels. TCP/UDP labels are preserved but not structured. |
| [Shepherd](https://github.com/djmaze/shepherd) | `shepherd.*` | Service auto-updater. Shows enable status and auth config. |
| [Swarm Cronjob](https://github.com/crazy-max/swarm-cronjob) | `swarm.cronjob.*` | Cron-scheduled jobs. Shows schedule, replica count, and skip/registry options. |
| [Diun](https://github.com/crazy-max/diun) | `diun.*` | Image update notifications. Shows watch settings, tag filters, and notification config. |
| [Cetacean ACL](authorization.md#label-based-access-control) | `cetacean.acl.*` | Resource-level access control. Shows read/write audience lists. |

## Editing

Integration panels support inline editing at [operations level](configuration.md#operations-level) 2 or higher. Each
field maps to its underlying Docker service label. A structured/raw toggle lets you switch between the form editor and
raw label key-value pairs.

## API

Integration data is included in the service detail response (`GET /services/{id}`) as an `integrations` array. See the
[API reference](/api) for the full schema.
