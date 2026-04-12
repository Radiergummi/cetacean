---
title: Recommendations
description: Automated cluster health checks for resource sizing, config hygiene, operational health, and topology.
category: guide
tags: [recommendations, sizing, health-checks, cluster-topology]
---

# Recommendations

The recommendation engine is an optional feature (enabled by default) that periodically evaluates cluster health and
surfaces actionable suggestions.

> **Note:** You can disable recommendations entirely with `server.recommendations = false`.

## Categories

Cetacean checks four domains. Checks that only need Docker state run every 60 seconds; checks that query Prometheus run
every 5 minutes.

| Category               | What it checks                                                                                                          | Requires                              |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------- | ------------------------------------- |
| **Resource Sizing**    | CPU/memory usage vs. configured limits and reservations (over-provisioned, approaching limit, at limit, missing limits) | Prometheus + cAdvisor                 |
| **Operational Health** | Flaky services (frequent task restarts), node disk/memory pressure                                                      | Prometheus + node-exporter + cAdvisor |
| **Config Hygiene**     | Missing health checks, missing restart policies                                                                         | —                                     |
| **Cluster Topology**   | Single-replica services, managers running workloads, uneven task distribution                                           | —                                     |

Where a safe fix is available (e.g., scaling a single-replica service to two, or adjusting resource limits to match
actual usage), the recommendation includes an **Apply** button that patches the service directly.

Without Prometheus, only config hygiene and cluster topology checks run. The rest degrade gracefully: no errors, just
fewer recommendations.

## Configuration

Sizing thresholds are configurable. Other checkers use fixed thresholds.

| Setting             | Config file key                       | Default | Description                                                 |
| ------------------- | ------------------------------------- | ------- | ----------------------------------------------------------- |
| Headroom multiplier | `sizing.headroom_multiplier`          | `2.0`   | Multiplier for suggested values                             |
| Over-provisioned    | `sizing.thresholds.over_provisioned`  | `0.20`  | Usage below this fraction of reservation → over-provisioned |
| Approaching limit   | `sizing.thresholds.approaching_limit` | `0.80`  | Usage above this fraction of limit → warning                |
| At limit            | `sizing.thresholds.at_limit`          | `0.95`  | Usage above this fraction of limit → critical               |
| Lookback            | `sizing.thresholds.lookback`          | `168h`  | Time window for p95 usage queries                           |

## API

`GET /recommendations`. See the [API reference](./api.md) for response schema.
