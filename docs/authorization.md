---
title: Authorization
description: Grant-based RBAC with per-resource access control, policy configuration, and provider grant sources.
category: guide
tags: [authorization, rbac, acl, grants, security]
---

# Authorization

Cetacean supports grant-based RBAC authorization that controls which resources each user can view and modify.
Authorization is independent of authentication — any auth provider can be combined with an ACL policy.

With no policy configured, all authenticated users have full access. With a policy, access is default-deny: only
explicitly granted resources are visible. Auth mode `none` bypasses authorization entirely.

## Grant Model

A grant is a tuple of **(resources, audience, permissions)**. All matching grants are unioned, there are no Deny rules.

```yaml
grants:
  - resources: [ "stack:webapp-*", "stack:api-*" ]
    audience: [ "group:engineering" ]
    permissions: [ "read", "write" ]

  - resources: [ "*" ]
    audience: [ "group:ops" ]
    permissions: [ "read", "write" ]

  - resources: [ "stack:public-*" ]
    audience: [ "user:*@example.com" ]
    permissions: [ "read" ]
```

**Resources** use `type:pattern` with glob wildcards (`*`, `?`). Supported types: `service`, `stack`, `node`, `task`,
`config`, `secret`, `network`, `volume`, `plugin`, `swarm`. Bare `*` matches all types. A `stack:X` grant covers the
stack and all its member resources. Tasks inherit from their parent service. Node grants use hostnames, not Docker IDs.

**Audience** uses `user:pattern` (matches subject and email) or `group:pattern` (matches group memberships). Bare `*`
matches everyone.

**Permissions** are `read` (view in lists, detail pages, SSE, search) and `write` (mutate; implies `read`).

## Policy Configuration

Policies can be provided as a file or inline. Inline takes precedence. Format is auto-detected (JSON, YAML, or TOML).

| Setting       | Env var                    | Config file key   | Description                                  |
|---------------|----------------------------|-------------------|----------------------------------------------|
| Inline policy | `CETACEAN_ACL_POLICY`      | `acl.policy`      | Policy string (requires restart to change)   |
| Policy file   | `CETACEAN_ACL_POLICY_FILE` | `acl.policy_file` | Path to policy file (hot-reloaded on change) |

File policies are watched for changes and swapped atomically. Invalid updates are logged and rejected, keeping the
previous policy in effect.

## Provider Grant Sources

Auth providers can also supply per-user grants directly, unioned with file policy. Provider grants are scoped to the
authenticated user (no `audience` field).

| Provider  | Source                       | Config                                   |
|-----------|------------------------------|------------------------------------------|
| Tailscale | CapMap peer capability       | `CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY` |
| OIDC      | Custom token claim           | `CETACEAN_AUTH_OIDC_ACL_CLAIM`           |
| Headers   | Proxy-injected header (JSON) | `CETACEAN_AUTH_HEADERS_ACL`              |
| Cert      | File policy only             | —                                        |
| None      | N/A (no authorization)       | —                                        |

## Examples

**Read-only observers:** everyone browses, only Ops writes:

```yaml
grants:
  - resources: [ "*" ]
    audience: [ "*" ]
    permissions: [ "read" ]
  - resources: [ "*" ]
    audience: [ "group:ops" ]
    permissions: [ "write" ]
```

**Team-scoped stacks:** each team manages their own stacks, shared infra is read-only:

```yaml
grants:
  - resources: [ "stack:frontend-*" ]
    audience: [ "group:frontend" ]
    permissions: [ "read", "write" ]
  - resources: [ "stack:api-*" ]
    audience: [ "group:backend" ]
    permissions: [ "read", "write" ]
  - resources: [ "stack:monitoring", "stack:ingress" ]
    audience: [ "*" ]
    permissions: [ "read" ]
```

**On-call with limited blast radius:** write services and tasks, read-only infra. Combine with `operations_level=1`:

```yaml
grants:
  - resources: [ "service:*", "task:*" ]
    audience: [ "group:oncall" ]
    permissions: [ "read", "write" ]
  - resources: [ "node:*", "swarm:*", "config:*", "secret:*", "network:*", "volume:*" ]
    audience: [ "group:oncall" ]
    permissions: [ "read" ]
```

**Multi-tenant isolation:** tenants see only their own stacks, no cross-visibility:

```yaml
grants:
  - resources: [ "stack:acme-*" ]
    audience: [ "group:tenant-acme" ]
    permissions: [ "read", "write" ]
  - resources: [ "stack:globex-*" ]
    audience: [ "group:tenant-globex" ]
    permissions: [ "read", "write" ]
```

After applying a policy, verify effective permissions with `curl -s localhost:9000/auth/whoami | jq .permissions`.

## Label-Based Access Control

In addition to policy files and provider grants, Cetacean can read access control directly from Docker resource labels.
Teams place `cetacean.acl.read` and `cetacean.acl.write` labels on their resources, and Cetacean enforces them without
requiring a central policy file.

### Enabling

| Setting      | Env var                | Config file key | Default | Description                       |
|--------------|------------------------|-----------------|---------|-----------------------------------|
| ACL labels   | `CETACEAN_ACL_LABELS`  | `acl.labels`    | `false` | Enable label-based ACL evaluation |

### Label Format

Labels use comma-separated audience expressions — the same `user:pattern` and `group:pattern` syntax as policy grants.

```yaml
services:
  myapp:
    deploy:
      labels:
        cetacean.acl.read: "group:*"
        cetacean.acl.write: "group:ops"
```

`cetacean.acl.read` grants read access; `cetacean.acl.write` grants write (and implies read). Multiple audiences are
comma-separated: `"group:frontend,user:alice@example.com"`.

### Supported Resources

Labels are evaluated on: `service`, `config`, `secret`, `network`, `volume`, `node`. Tasks inherit from their parent
service. Stacks have no labels of their own — use a config policy for stack-wide grants.

### Precedence Rules

Labels are checked before config grants. The outcome depends on whether the identity matches any label audience:

| Scenario | Result |
|---|---|
| Identity matches label audience, label grants `write` | Allowed (`write`) |
| Identity matches label audience, label grants `read` only | Allowed (`read`), write denied |
| Identity does not match label audience, has explicit config/provider grant | Config/provider grant applies |
| Identity does not match label audience, no explicit config grant | Denied (labels suppress implicit allow-all) |
| Labels are absent | Normal policy evaluation (config grants or allow-all default) |

Within the label layer, the most permissive match wins (additive). Presence of any `cetacean.acl.*` label on a resource
suppresses the implicit allow-all for that resource — unauthenticated-equivalent access no longer applies, even when no
policy file is configured.

## Interaction with Operations Level

ACL and [operations level](configuration.md#operations-level) are independent checks; both must pass for a write
operation to succeed. Operations level is a global ceiling (which *categories* of writes are enabled), while ACL
controls which *resources* each user can modify. A common pattern is `operations_level=1` (safe ops only) combined with
ACL grants for per-team scoping.

| Scenario                                    | Result                |
|---------------------------------------------|-----------------------|
| Operations level allows, ACL grants `write` | Allowed               |
| Operations level allows, ACL denies `write` | Denied (`403 ACL002`) |
| Operations level blocks, ACL grants `write` | Denied (`403 OPS001`) |
