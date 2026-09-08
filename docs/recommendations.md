---
title: Recommendations
description: What Cetacean flags about your cluster, how to act on it, and how to tune or turn it off.
category: guide
tags: [recommendations, sizing, health-checks, cluster-topology]
---

# Recommendations

Cetacean watches your cluster and lists what looks wrong at `/recommendations` in the [dashboard][dashboard] —
services with no health check, replicas that keep dying, containers sized far above what they use. Findings
refresh on their own; you never trigger a scan.

It is on by default. Turn it off with [`server.recommendations`][server.recommendations].

## What gets flagged

Each finding names a service, a node or the cluster, and carries a severity of `info`, `warning` or
`critical`.

| Finding | Meaning |
|---|---|
| `no-healthcheck` | The service defines no health check, so Swarm cannot tell a hung container from a working one |
| `no-restart-policy` | Restart policy is `none`, so a failed task is never replaced |
| `single-replica` | One replica, so any node problem is an outage |
| `manager-has-workloads` | A manager node is `active` and running ordinary tasks alongside cluster management |
| `uneven-distribution` | The busiest node runs more than three times the tasks of the quietest |
| `flaky-service` | More than 5 involuntary task failures within the lookback window |
| `node-disk-full` | Node disk above 90% |
| `node-memory-pressure` | Node memory above 90% |
| `over-provisioned` | Reserved far more CPU or memory than it uses |
| `approaching-limit` | Usage climbing towards its limit |
| `at-limit` | Usage at its limit, so it is being throttled or is at risk of being killed |
| `no-limits`, `no-reservations` | No limit or reservation set, so Swarm cannot schedule it well |

## Act on a finding

Findings that have an obvious fix get an **Apply suggested value** button:

<!-- cards -->
| Finding | What the button does | Needs |
|---|---|---|
| `over-provisioned` | Raises or lowers the reservation to match real usage | [Operations level][operations-level] 2 |
| `approaching-limit`, `at-limit` | Adjusts the limit | [Operations level][operations-level] 2 |
| `single-replica` | Scales the service to 2 replicas | [Operations level][operations-level] 1 |

All three also need [write permission][authorization] on the service. Without it the request fails and the error
appears above the list. Everything else is reported for you to act on yourself.

## Without Prometheus

Cetacean still flags configuration and topology problems, and still catches flaky services, using Docker state
alone. What disappears is everything measured: the whole sizing group, plus `node-disk-full` and
`node-memory-pressure`. Nothing errors—you simply see fewer findings.

Sizing needs cAdvisor and the node checks need node-exporter. See [Monitoring][monitoring] for the setup.

## Tune the sizing thresholds

Sizing is the only group you can tune, through
[`sizing.thresholds.over_provisioned`][sizing.thresholds.over_provisioned],
[`sizing.thresholds.approaching_limit`][sizing.thresholds.approaching_limit] and
[`sizing.thresholds.at_limit`][sizing.thresholds.at_limit]—the fractions of the reservation or limit at
which each finding appears. [`sizing.headroom_multiplier`][sizing.headroom_multiplier] sets how much room the
suggested value leaves above observed usage, and [`sizing.thresholds.lookback`][sizing.thresholds.lookback]
how far back Cetacean looks, which is also the window `flaky-service` counts failures in.

## API

`GET /recommendations` returns the current findings and a severity summary; the [MCP][mcp-tools]
`get_recommendations` tool serves the same data. See the [API reference][api] for the response schema.

[api]: api
[authorization]: authorization
[dashboard]: dashboard
[mcp-tools]: mcp-tools
[monitoring]: monitoring
[operations-level]: configuration#operations-level
[server.recommendations]: configuration#server.recommendations
[sizing.headroom_multiplier]: configuration#sizing.headroom_multiplier
[sizing.thresholds.approaching_limit]: configuration#sizing.thresholds.approaching_limit
[sizing.thresholds.at_limit]: configuration#sizing.thresholds.at_limit
[sizing.thresholds.lookback]: configuration#sizing.thresholds.lookback
[sizing.thresholds.over_provisioned]: configuration#sizing.thresholds.over_provisioned
