---
title: Integrations
description: Structured panels for Traefik, Caddy, Shepherd, Gantry, swarm-cronjob, Diun, and doco-cd on the service detail page.
category: guide
tags: [integrations, traefik, caddy, shepherd, gantry, swarm-cronjob, diun, doco-cd]
---

# Integrations

Cetacean recognizes a few Swarm ecosystem tools, such as Traefik and Shepherd, from a service's own labels and
renders each as a structured panel on the service [detail page][detail-pages], above the labels section. Nothing is
rendered when no matching labels are present.

## Supported tools

A tool is detected when the service carries at least one label with its prefix.

<!-- cards -->
| Tool                                                                    | Label prefix     | Panel shows                                                                       |
|-------------------------------------------------------------------------|------------------|-----------------------------------------------------------------------------------|
| [Traefik](https://traefik.io/)                                          | `traefik.`       | HTTP routers, services, and middlewares parsed from `traefik.http.*`              |
| [Caddy](https://github.com/lucaslorentz/caddy-docker-proxy)             | `caddy`          | The Caddyfile that caddy-docker-proxy builds from the labels                      |
| [Shepherd](https://github.com/djmaze/shepherd)                          | `shepherd.`      | Enable state and `shepherd.auth.config`                                           |
| [Gantry](https://github.com/shizunge/gantry)                            | `gantry.`        | Update, rollback, and manifest settings that override Gantry's defaults           |
| [swarm-cronjob](https://github.com/crazy-max/swarm-cronjob)             | `swarm.cronjob.` | Schedule, replicas, skip-running, and the two registry options                    |
| [Diun](https://github.com/crazy-max/diun)                               | `diun.`          | Watch settings, tag include/exclude/sort filters, platform, and `diun.metadata.*` |
| [doco-cd](https://github.com/kimdre/doco-cd)                            | `cd.doco.`       | The repository, ref, and commit the service was deployed from, and its schedule   |

A panel shows the tool as enabled unless a `<prefix>enable` label says otherwise. Gantry has no such label, and
shows as disabled when `gantry.services.excluded` is `true`.

The Caddy panel matches the labels caddy-docker-proxy itself reads: `caddy`, `caddy_0`, `caddy.reverse_proxy`, but
not `caddy_ingress_network`. Template expressions such as `{{upstreams 8080}}` are shown as written, not evaluated.
A label prefix changed through `CADDY_DOCKER_LABEL_PREFIX` isn't recognized.

The doco-cd panel links the deployed commit when the host of the source URL names GitHub, GitLab, Gitea, Forgejo, or
Codeberg, and shows the commit as text otherwise. Credentials in the source URL are left out of the panel.

Traefik's TCP and UDP labels are left to the raw label view; only the HTTP routers, services, and middlewares are
parsed into structure.

The Traefik panel adds a read-only **Graph** view, drawing each entrypoint through the routers listening on it and
their middleware chains to the service they route to.

## Editing

Each panel has a view toggle and a link to the tool's own documentation. The structured and raw views are editable
when the service's detail response links its labels with `rel="edit"`, which requires
[operations level][operations-level] 2 and a write grant on the service. Saving writes the labels back through `PATCH /services/{id}/labels`; every field maps to one Docker
service label.

The Caddy and doco-cd panels have no form: doco-cd rewrites its labels on every deploy, and Caddy's labels are a
configuration language of their own. Editing either opens the raw label view.

The structured Traefik editor changes only the labels its fields show. Labels it doesn't model, such as TCP
routers or `tls.options`, are kept when you save it.

## API

Detected integrations appear as an `integrations` array on `GET /services/{id}`, omitted when nothing was detected.
See the [API reference][api] for the full schema.

[api]: api
[detail-pages]: dashboard#detail-pages
[operations-level]: configuration#operations-level
