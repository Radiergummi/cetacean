---
title: Recommendations
description: Automated cluster health checks for resource sizing, config hygiene, operational health, and topology.
category: guide
tags: [recommendations, sizing, health-checks, cluster-topology]
---

# Recommendations

The recommendation engine evaluates cluster health on a timer and lists what it finds at `/recommendations` in the
dashboard. It is enabled by default; disable it with `server.recommendations = false`.

## Checkers

Four checkers run independently. The engine runs all of them once at startup, then ticks every 60 seconds and runs
each checker whose own interval has elapsed. A single check is cancelled after 30 seconds.

| Checker         | Interval | Detects                                                                                              | Needs Prometheus                 |
| --------------- | -------- | ---------------------------------------------------------------------------------------------------- | -------------------------------- |
| `config`        | 60s      | Services with no health check and services with restart policy `none`                                | No                               |
| `cluster`       | 60s      | Single-replica services, manager nodes with `active` availability, uneven task distribution           | No                               |
| `operational`   | 5m       | Flaky services; node disk and memory usage above 90%                                                  | Flaky services no, the rest yes  |
| `sizing`        | 5m       | CPU and memory usage against configured limits and reservations, and missing limits and reservations   | Yes                              |

Findings carry a category, a severity (`info`, `warning`, `critical`), and a scope (`service`, `node`, `cluster`):

| Checker       | Categories                                                                                  |
| ------------- | ------------------------------------------------------------------------------------------- |
| `config`      | `no-healthcheck`, `no-restart-policy`                                                       |
| `cluster`     | `single-replica`, `manager-has-workloads`, `uneven-distribution`                            |
| `operational` | `flaky-service`, `node-disk-full`, `node-memory-pressure`                                   |
| `sizing`      | `over-provisioned`, `approaching-limit`, `at-limit`, `no-limits`, `no-reservations`          |

Fixed thresholds worth knowing: a service is flaky after more than 5 involuntary task failures inside the sizing
lookback window, a node is flagged above 90% disk or memory usage, and task distribution counts as uneven when the
busiest node runs more than three times the tasks of the least busy one.

## Without Prometheus

The `config` and `cluster` checkers read only Docker state and always run. The `operational` checker also always
runs, but reports flaky services alone and skips the node disk and memory queries. The `sizing` checker is not
registered at all, so no sizing category appears. Nothing errors; you get fewer findings.

Sizing needs cAdvisor for container metrics and the operational node checks need node-exporter. See
[Monitoring](monitoring) for the scrape requirements.

## Applying a fix

A finding that carries both a suggested value and a write endpoint gets an **Apply suggested value** button that
patches the service directly. That covers `over-provisioned` (which raises or lowers the reservation),
`approaching-limit` and `at-limit` (which adjust the limit) through `PATCH /services/{id}/resources`, and
`single-replica` through `PUT /services/{id}/scale` with 2 replicas. The other categories report only.

The button is shown whenever a suggestion exists; the request behind it is gated like any other write. Patching
resources needs [operations level](configuration#operations-level) 2, scaling needs level 1, and both need ACL write
permission on the service. Without them the request fails with 403 and the error appears above the list.

## Sizing thresholds

The sizing checker is the only one with configurable thresholds. Full descriptions and accepted ranges are in
[Configuration](configuration).

| Setting                               | Env var                                      | Default | Effect                                                        |
| ------------------------------------- | -------------------------------------------- | ------- | ------------------------------------------------------------- |
| `sizing.headroom_multiplier`          | `CETACEAN_SIZING_HEADROOM_MULTIPLIER`        | `2.0`   | Multiplier applied to observed usage when suggesting a value   |
| `sizing.thresholds.over_provisioned`  | `CETACEAN_SIZING_THRESHOLD_OVER_PROVISIONED` | `0.20`  | Usage below this fraction of the reservation is over-provisioned |
| `sizing.thresholds.approaching_limit` | `CETACEAN_SIZING_THRESHOLD_APPROACHING_LIMIT`| `0.80`  | Usage above this fraction of the limit is a warning            |
| `sizing.thresholds.at_limit`          | `CETACEAN_SIZING_THRESHOLD_AT_LIMIT`         | `0.95`  | Usage above this fraction of the limit is critical             |
| `sizing.thresholds.lookback`          | `CETACEAN_SIZING_LOOKBACK`                   | `168h`  | p95 usage window, and the window the flaky-service count covers |

## API

`GET /recommendations` returns the current findings and a severity summary. The MCP `get_recommendations` tool serves
the same data. See the [API reference](api) for the response schema.
