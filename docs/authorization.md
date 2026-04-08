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
Teams place `cetacean.acl.read` and `cetacean.acl.write` labels on their resources to control who can see and modify
them — no central policy file required.

| Setting    | Env var               | Config file key | Default | Description                       |
|------------|-----------------------|-----------------|---------|-----------------------------------|
| ACL labels | `CETACEAN_ACL_LABELS` | `acl.labels`    | `false` | Enable label-based ACL evaluation |

### Label Format

Labels use the same `user:pattern` and `group:pattern` audience syntax as policy grants, comma-separated.
`cetacean.acl.write` implies `cetacean.acl.read`.

```yaml
services:
  myapp:
    deploy:
      labels:
        cetacean.acl.read: "group:frontend,user:alice@example.com"
        cetacean.acl.write: "group:ops"
```

Labels are evaluated on `service`, `config`, `secret`, `network`, `volume`, and `node` resources. Tasks inherit from
their parent service. Stacks have no labels of their own — use a config policy for stack-wide grants.

### How Labels Interact with Policy

Labels and config policy are two independent layers. When both exist, labels take priority for identities they mention;
config policy fills in the rest:

1. **Identity matches a label audience** → label determines the permission. Even if the config policy grants more, the
   label result wins for that identity on that resource.
2. **Identity does not match any label audience, but has an explicit config/provider grant** → the config grant applies.
3. **Identity does not match any label audience, and has no config grant** → denied. The presence of labels on a
   resource disables the implicit allow-all default for that resource, even when no policy file is configured.
4. **No labels on the resource** → normal policy evaluation (config grants or allow-all default).

Within labels, the most permissive match wins (additive, same as policy grants).

### Examples

**Restrict a sensitive service without a policy file.** No policy is configured (allow-all by default). Adding a label
limits who can see one specific service while everything else remains open:

```yaml
services:
  admin-dashboard:
    deploy:
      labels:
        cetacean.acl.read: "group:ops"
        cetacean.acl.write: "group:ops"
```

All other services remain visible to everyone. Only `admin-dashboard` requires the `ops` group.

**Team-owned services with broad read access.** Everyone can view; only the owning team can modify:

```yaml
services:
  checkout:
    deploy:
      labels:
        cetacean.acl.read: "group:*"
        cetacean.acl.write: "group:commerce"
```

**Labels combined with a policy file.** A config policy grants `write` on `service:*` to `group:dev`. A control-plane
service narrows that down:

```yaml
# In the compose file
services:
  cetacean:
    deploy:
      labels:
        cetacean.acl.read: "group:*"
        cetacean.acl.write: "group:ops"
```

Result: `dev` users can read `cetacean` (they match the `group:*` label audience) but cannot write it (the label
overrides their config grant). They can still write all other services via their config policy. An `ops` user can write
`cetacean` via the label grant. A CI bot with an explicit config grant for `service:cetacean` can still write it because
explicit config grants apply when the identity is not mentioned in the labels.

### Security Consideration

Anyone who can deploy a stack can set labels on their services. With label-based ACL enabled, this means stack deployers
can broaden access to their own resources — for example, `cetacean.acl.write: "*"` would grant write to everyone. Labels
cannot affect other resources: they are strictly scoped to the resource they are set on.

If this self-service model is too permissive, use a config policy file to set the access boundaries and leave labels
disabled.

### Verifying Effective Permissions

`GET /auth/whoami` shows permissions from config and provider grants only — it cannot reflect label-based permissions
because those depend on which resource is being accessed.

To check label-based permissions for a specific resource, inspect the `Allow` header on a detail endpoint:

```bash
curl -s -I localhost:9000/services/abc123 | grep Allow
# Allow: GET, HEAD                          → read-only (labels restrict write)
# Allow: GET, HEAD, PUT, POST, PATCH        → read + write
```

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
