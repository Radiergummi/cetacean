---
title: Integrations
description: Structured panels for Traefik, Caddy, Shepherd, Gantry, swarm-cronjob, Diun, and doco-cd on the service detail page, and custom links from labels.
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

The doco-cd panel links the deployed commit when the source URL is on `github.com`, `gitlab.com`, `gitea.com`, or
`codeberg.org`, or on a host whose first label names the forge, such as `gitlab.example.com`. Otherwise it shows the
commit as text. The structured view leaves credentials out of the source URL, and omits a URL it can't parse; the
raw label still holds it as written.

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

## Custom links

A service can link to its dashboard, runbook, or repository through its own labels, so the links live in the stack
file next to the service:

```yaml
services:
  web:
    deploy:
      labels:
        cetacean.link.grafana: https://grafana.example.com/d/web
        cetacean.link.grafana.title: Dashboard
        cetacean.link.runbook: https://wiki.example.com/runbooks/web
```

Each `cetacean.link.<id>` label adds one link, and the optional `cetacean.link.<id>.title` names it; without one,
the link reads as `<id>`. Links are sorted by `<id>`, and a title without a matching link is ignored, as is a value
that is empty or only whitespace. The labels belong under `deploy.labels`, on the service rather than its containers.

The service detail page shows the links under its title, and the stack detail page the links of all its services,
with exact duplicates merged. Where two links on a stack share a title, each also names the services declaring it.
A value that is an `http` or `https` URL becomes a link, opening in a new tab, with any character a URL can't hold
percent-encoded. Any other value is shown as plain text, and so is a URL with a user name or password in it.

## API

Detected integrations appear as an `integrations` array on `GET /services/{id}`, omitted when nothing was detected.
Each one names the service `labels` it was read from, which the dashboard leaves out of the generic labels section.
Custom links appear as a `links` array on `GET /services/{id}` and `GET /stacks/{name}`, each with an `id`, a
`title`, and either an `href` for an `http` or `https` URL or a `text` for anything else. A stack's links also name
the `services` declaring each one. See the [API reference][api] for the full schema.

[api]: api
[detail-pages]: dashboard#detail-pages
[operations-level]: configuration#operations-level
