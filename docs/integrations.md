---
title: Integrations
description: Structured panels for Traefik, Shepherd, Swarm Cronjob, and Diun on the service detail page.
category: guide
tags: [integrations, traefik, shepherd, swarm-cronjob, diun]
---

# Integrations

Cetacean recognises four Swarm ecosystem tools — and its own access-control labels — from a service's own labels,
and renders each as a structured panel on the service detail page, above the labels section. Nothing is rendered
when no matching labels are present.

## Supported tools

A tool is detected when the service carries at least one label with its prefix.

| Tool                                                        | Label prefix      | Panel shows                                                                     |
| ----------------------------------------------------------- | ----------------- | ------------------------------------------------------------------------------- |
| [Traefik](https://traefik.io/)                              | `traefik.`        | HTTP routers, services, and middlewares parsed from `traefik.http.*`            |
| [Shepherd](https://github.com/djmaze/shepherd)              | `shepherd.`       | Enable state and `shepherd.auth.config`                                         |
| [Swarm Cronjob](https://github.com/crazy-max/swarm-cronjob) | `swarm.cronjob.`  | Schedule, replicas, skip-running, and the two registry options                  |
| [Diun](https://github.com/crazy-max/diun)                   | `diun.`           | Watch settings, tag include/exclude/sort filters, platform, and `diun.metadata.*` |
| [Cetacean ACL](authorization#label-based-access-control)    | `cetacean.acl.`   | The read and write audiences the resource's own labels grant                     |

For each tool, `<prefix>enable` sets the enabled state shown in the panel. When that label is absent Cetacean
reports the integration as enabled, which is always the case for the ACL panel — it has no enable label, and is
shown whenever a `cetacean.acl.*` label is present. Whether those labels are *enforced* is a separate question,
settled by `acl.labels`; the panel shows what the labels say either way.

Only `traefik.http.routers.*`, `traefik.http.services.*`, and `traefik.http.middlewares.*` are parsed into structure.
`traefik.tcp.*` and `traefik.udp.*` labels are left to the raw label view.

## Editing

Each panel has a structured/raw toggle and a link to the tool's own documentation. Both views are editable when the
service allows `PATCH`, which requires [operations level](configuration#operations-level) 2 and ACL write permission
on the service. Saving writes the labels back through `PATCH /services/{id}/labels`; every field maps to one Docker
service label.

## API

Detected integrations appear as an `integrations` array on `GET /services/{id}`, omitted when nothing was detected.
See the [API reference](api) for the full schema.
