---
title: Integrations
description: Structured panels for Traefik, Shepherd, swarm-cronjob, and Diun on the service detail page.
category: guide
tags: [ integrations, traefik, shepherd, swarm-cronjob, diun ]
---

# Integrations

Cetacean has support for certain Swarm ecosystem tools, like Traefik or Shepherd. It detects them
from a service's own labels, and renders each as a structured panel on the service
[detail page][detail-pages], above the labels section. Nothing is rendered when no matching labels
are present.

## Supported tools

A tool is detected when the service carries at least one label with its prefix.

<!-- cards -->
| Tool                                                        | Label prefix     | Panel shows                                                                       |
|-------------------------------------------------------------|------------------|-----------------------------------------------------------------------------------|
| [Traefik](https://traefik.io/)                              | `traefik.`       | HTTP routers, services, and middlewares parsed from `traefik.http.*`              |
| [Shepherd](https://github.com/djmaze/shepherd)              | `shepherd.`      | Enable state and `shepherd.auth.config`                                           |
| [swarm-cronjob](https://github.com/crazy-max/swarm-cronjob) | `swarm.cronjob.` | Schedule, replicas, skip-running, and the two registry options                    |
| [Diun](https://github.com/crazy-max/diun)                   | `diun.`          | Watch settings, tag include/exclude/sort filters, platform, and `diun.metadata.*` |

A panel shows the tool as enabled unless a `<prefix>enable` label says otherwise.

Traefik's TCP and UDP labels are left to the raw label view; only the HTTP routers, services, and middlewares are
parsed into structure.

The Traefik panel adds a read-only **Graph** view, drawing each entrypoint through the routers listening on it and
their middleware chains to the service they route to.

## Editing

Each panel has a view toggle and a link to the tool's own documentation. The structured and raw views are editable
when the service allows `PATCH`, which requires [operations level][operations-level] 2 and ACL write permission
on the service. Saving writes the labels back through `PATCH /services/{id}/labels`; every field maps to one Docker
service label.

## API

Detected integrations appear as an `integrations` array on `GET /services/{id}`, omitted when nothing was detected.
See the [API reference][api] for the full schema.

[api]: api
[detail-pages]: dashboard#detail-pages
[operations-level]: configuration#operations-level
