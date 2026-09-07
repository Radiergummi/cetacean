---
title: Authorization
description: Grant-based access control with resource patterns, audiences, policy files, and provider grant sources.
category: guide
tags: [authorization, rbac, acl, grants, security]
---

# Authorization

Cetacean decides which resources an identity may view and change with grant-based access control. It combines with
any [authentication](authentication) provider.

Three rules decide how much access an identity has:

- `auth.mode` is `none`: authorization is bypassed. A policy configured in this mode has no effect.
- No policy configured: every authenticated identity has full access.
- A policy configured: access is default-deny. An identity sees only what a grant covers.

## Grants

A grant is a tuple of resources, audience and permissions. Every grant whose audience matches the identity applies,
and their permissions are unioned. There are no deny rules, so adding a grant can only widen access.

```yaml
grants:
  - resources: ["stack:webapp-*", "stack:api-*"]
    audience: ["group:engineering"]
    permissions: ["read", "write"]

  - resources: ["*"]
    audience: ["group:ops"]
    permissions: ["read", "write"]

  - resources: ["stack:public-*"]
    audience: ["user:*@example.com"]
    permissions: ["read"]
```

### Resources

A resource expression is `type:pattern`. Valid types are `service`, `stack`, `node`, `task`, `config`, `secret`,
`network`, `volume`, `plugin` and `swarm`. A bare `*` matches every resource of every type; every other expression
must name a type. Patterns are globs (`*`, `?`, `[...]`) matched against the identifier the resource is keyed by:

| Type                                                                  | Pattern matches                            |
| --------------------------------------------------------------------- | ------------------------------------------ |
| `service`, `stack`, `config`, `secret`, `network`, `volume`, `plugin` | Resource name                              |
| `node`                                                                | Hostname, or the node ID if it has none    |
| `task`                                                                | Task ID                                    |
| `swarm`                                                               | The single resource `swarm:cluster`        |

Two inheritance rules widen a grant beyond a literal match:

- A `stack:X` grant covers the stack and every service, task, config, secret, network and volume in it.
- A `service:X` grant covers that service's tasks. A task also inherits the stack of its parent service.

Task patterns match task IDs, which change every time a replica is replaced. Grant the parent service or the stack
instead of naming tasks.

### Audience

`user:pattern` matches the identity's subject or email. `group:pattern` matches any of its groups. Patterns are globs
again, a bare `*` matches everyone, and a grant with no `audience` field also matches everyone.

### Permissions

`read` and `write`. `write` implies `read`, so a write grant needs no separate read grant.

`read` governs list and detail endpoints, search, history, Atom and JSON feeds, topology, recommendations and SSE
streams. `write` governs every mutation. Lists never fail on a missing grant: unreadable items are filtered out of
the response, and `total` counts only what the identity may see.

## Policy configuration

Provide the policy inline or as a file. Neither setting has a CLI flag; set them through the environment or the
config file. Inline takes precedence when both are set.

| Setting          | Env var                    | Description                                              |
| ---------------- | -------------------------- | -------------------------------------------------------- |
| `acl.policy`     | `CETACEAN_ACL_POLICY`      | Inline policy string. Changing it requires a restart     |
| `acl.policy_file` | `CETACEAN_ACL_POLICY_FILE` | Path to a policy file, hot-reloaded when the file changes |

An inline policy is parsed as JSON, TOML or YAML by auto-detection. A policy file is parsed by its extension
(`.json`, `.toml`, `.yaml`, `.yml`), falling back to auto-detection for any other extension.

A malformed or invalid policy at startup stops the server. A policy file is watched and swapped atomically on change;
an invalid update is logged and rejected, leaving the previous policy in force. Cetacean logs a warning if the policy
file is world-readable.

An empty grant list (`grants: []`) is valid and denies everything.

## Provider grant sources

An auth provider can carry per-user grants on the identity itself. These complement the policy rather than replacing
it: the identity's own grants are added to whatever the policy already grants it. Only the active auth mode's source
is read.

| Provider    | Source                       | Setting                    | Env var                                  |
| ----------- | ---------------------------- | -------------------------- | ---------------------------------------- |
| `tailscale` | Peer capability in the CapMap | `acl.tailscale_capability` | `CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY` |
| `oidc`      | Custom token claim           | `acl.oidc_claim`           | `CETACEAN_AUTH_OIDC_ACL_CLAIM`           |
| `headers`   | Proxy-injected header (JSON) | `acl.headers_acl`          | `CETACEAN_AUTH_HEADERS_ACL`              |
| `cert`      | Policy only                  | —                          | —                                        |
| `none`      | Not applicable               | —                          | —                                        |

Each source expects a JSON array of grant objects carrying `resources` and `permissions`. `audience` is ignored:
provider grants are always scoped to the identity that carried them. A grant that fails validation is dropped
silently, so check the policy path first when a grant appears to have no effect.

> **Note:** OIDC browser sessions do not carry raw token claims, so `acl.oidc_claim` grants apply to Bearer-token
> requests only. Grant browser users through the policy, matching on `group:` audiences.

## Interaction with operations level

[Operations level](configuration#operations-level) and grants are independent checks, and a write needs both to
pass. Operations level is a global ceiling on which categories of write the server exposes at all; grants decide
which resources a given identity may write. A common pairing is `server.operations_level = 1` with per-team grants.

| Operations level | Grant           | Result                |
| ---------------- | --------------- | --------------------- |
| Allows           | Grants `write`  | Allowed               |
| Allows           | No `write`      | Denied (`403 ACL002`) |
| Blocks           | Grants `write`  | Denied (`403 OPS001`) |

Denied requests answer with an [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem document:

| Code     | Status | Meaning                                                                  |
| -------- | ------ | ------------------------------------------------------------------------ |
| `ACL001` | 403    | Read denied on a detail endpoint, or the identity holds no grants at all |
| `ACL002` | 403    | Write denied on this resource                                            |
| `OPS001` | 403    | The operation needs a higher operations level than the server runs at    |

`GET` and `HEAD` responses carry an `Allow` header listing the write methods available for that resource, combining
the operations level with the identity's grants. The dashboard uses it to decide which action buttons to show.

Prometheus query endpoints (`GET /metrics`, `GET /metrics/labels`) are not per-resource filtered. They require the
identity to hold at least one grant.

## Examples

Everyone browses, only ops writes:

```yaml
grants:
  - resources: ["*"]
    audience: ["*"]
    permissions: ["read"]
  - resources: ["*"]
    audience: ["group:ops"]
    permissions: ["write"]
```

Team-scoped stacks, with shared infrastructure readable by all:

```yaml
grants:
  - resources: ["stack:frontend-*"]
    audience: ["group:frontend"]
    permissions: ["read", "write"]
  - resources: ["stack:api-*"]
    audience: ["group:backend"]
    permissions: ["read", "write"]
  - resources: ["stack:monitoring", "stack:ingress"]
    audience: ["*"]
    permissions: ["read"]
```

On-call with a limited blast radius: writes services and tasks, reads everything else. Pair it with
`server.operations_level = 1`.

```yaml
grants:
  - resources: ["service:*", "task:*"]
    audience: ["group:oncall"]
    permissions: ["read", "write"]
  - resources:
      ["node:*", "swarm:*", "config:*", "secret:*", "network:*", "volume:*"]
    audience: ["group:oncall"]
    permissions: ["read"]
```

Multi-tenant isolation, with no cross-tenant visibility:

```yaml
grants:
  - resources: ["stack:acme-*"]
    audience: ["group:tenant-acme"]
    permissions: ["read", "write"]
  - resources: ["stack:globex-*"]
    audience: ["group:tenant-globex"]
    permissions: ["read", "write"]
```

## Verifying a policy

`GET /profile` returns the current identity together with the grant patterns in effect for it:

```bash
curl -s -H "Accept: application/json" http://localhost:9000/profile | jq .permissions
```

The response projects the raw grant patterns, not the resources they resolve to. To check a specific resource, read
its detail endpoint and inspect the `Allow` header.

The [MCP server](mcp) applies the same grants. Its tools and resources are filtered per caller, and a tool the
caller can never use is left out of `tools/list`.
