# Authorization

Cetacean supports grant-based RBAC authorization that controls which resources each user can view and modify. Authorization is independent of authentication -- any auth provider can be combined with an ACL policy.

### Default Behavior

| Condition | Result |
|---|---|
| Auth mode `none` | No authorization, full access |
| Auth provider active, no policy | Full access for all authenticated users |
| Auth provider active, policy configured | Default-deny; only explicitly granted access |

### Grant Model

A grant is a tuple of **(resources, audience, permissions)**. All matching grants are unioned -- there are no deny rules.

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

#### Resource Expressions

Format: `type:pattern` with glob wildcards (`*`, `?`).

| Type | Example |
|---|---|
| `service` | `service:webapp-api`, `service:*` |
| `stack` | `stack:monitoring`, `stack:webapp-*` |
| `node` | `node:worker-*`, `node:orb-1` |
| `task` | `task:*` (tasks also inherit from parent service) |
| `config` | `config:traefik-*` |
| `secret` | `secret:db-*` |
| `network` | `network:frontend` |
| `volume` | `volume:data-*` |
| `plugin` | `plugin:*` |
| `swarm` | `swarm:cluster` |

Bare `*` is shorthand for all resources of all types.

A `stack:X` grant covers the stack itself and all its member resources (services, configs, secrets, networks, volumes). Tasks inherit permissions from their parent service.

Node grants use hostnames (e.g., `node:worker-*`), not Docker node IDs.

#### Audience Expressions

Format: `kind:pattern` with glob wildcards.

- `user:pattern` -- matches against both `Identity.Subject` and `Identity.Email`
- `group:pattern` -- matches against each entry in `Identity.Groups`
- Bare `*` matches everyone

Examples: `user:alice@example.com`, `user:*@example.com`, `group:eng-*`, `group:ops`

#### Permissions

- `read` -- view the resource in lists, detail pages, SSE streams, and search
- `write` -- mutate the resource (implies `read`)

### Policy Configuration

Policies can be provided as a file or inline. File policies support hot reload -- changes take effect without restarting.

| Setting | Env var | Config file key | Description |
|---|---|---|---|
| Inline policy | `CETACEAN_ACL_POLICY` | `acl.policy` | JSON, YAML, or TOML policy string |
| Policy file | `CETACEAN_ACL_POLICY_FILE` | `acl.policy_file` | Path to policy file (watched for changes) |

Inline policy takes precedence over policy file. File format is detected by extension (`.json`, `.yaml`/`.yml`, `.toml`); inline policy auto-detects format.

#### Hot Reload

When using `acl.policy_file`, changes to the file are detected automatically:

- Valid new policy: swapped atomically, logged
- Invalid new policy: error logged, previous policy kept
- Inline policy (`acl.policy`) requires a restart to change

### Provider Grant Sources

In addition to file-based policy, auth providers can supply per-user grants directly. Provider grants and file grants are unioned -- if either source grants access, the user has it.

| Provider | Source | Config |
|---|---|---|
| Tailscale | CapMap peer capability | `CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY` |
| OIDC | Custom token claim | `CETACEAN_AUTH_OIDC_ACL_CLAIM` |
| Headers | Proxy-injected header (JSON) | `CETACEAN_AUTH_HEADERS_ACL` |
| Cert | File policy only | -- |
| None | N/A (no authorization) | -- |

Provider grants omit the `audience` field -- they are implicitly scoped to the authenticated user.

### Policy Examples

#### Read-only observers

Everyone can browse, but only the ops group can make changes:

```yaml
grants:
  - resources: ["*"]
    audience: ["*"]
    permissions: ["read"]

  - resources: ["*"]
    audience: ["group:ops"]
    permissions: ["write"]
```

#### Team-scoped stacks

Each team can view and manage their own stacks. A `stack:X` grant covers the stack and all its member services,
configs, secrets, networks, and volumes. Shared infrastructure is read-only for everyone:

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

#### Secrets restricted to platform team

Developers can manage services but never view secrets or configs. The platform team has full access:

```yaml
grants:
  - resources: ["service:*", "stack:*", "node:*", "task:*", "network:*", "volume:*"]
    audience: ["group:developers"]
    permissions: ["read", "write"]

  - resources: ["*"]
    audience: ["group:platform"]
    permissions: ["read", "write"]
```

Note that developers can still see services that _reference_ a secret (the cross-reference list), but not the secret
detail page itself.

#### On-call with limited blast radius

On-call engineers can scale, restart, and rollback any service (requires `write`), but cluster-level resources like
nodes and swarm config are view-only. Combine with `operations_level=1` to restrict to reactive ops only:

```yaml
grants:
  - resources: ["service:*", "task:*"]
    audience: ["group:oncall"]
    permissions: ["read", "write"]

  - resources: ["node:*", "swarm:*", "config:*", "secret:*", "network:*", "volume:*"]
    audience: ["group:oncall"]
    permissions: ["read"]
```

#### Multi-tenant isolation

Each tenant sees only their resources. No cross-visibility, no global wildcard. Name stacks with a tenant prefix:

```yaml
grants:
  - resources: ["stack:acme-*"]
    audience: ["group:tenant-acme"]
    permissions: ["read", "write"]

  - resources: ["stack:globex-*"]
    audience: ["group:tenant-globex"]
    permissions: ["read", "write"]
```

Tenants cannot see each other's stacks, services, or infrastructure. Shared nodes and networks are not visible unless
explicitly granted.

#### Single user with full access

For small deployments with one admin and several viewers:

```yaml
grants:
  - resources: ["*"]
    audience: ["user:admin@example.com"]
    permissions: ["read", "write"]

  - resources: ["*"]
    audience: ["*"]
    permissions: ["read"]
```

#### Testing a policy

After applying a policy, check effective permissions for the current user:

```bash
curl -s http://localhost:9000/auth/whoami | jq .permissions
```

The `permissions` field shows the resource patterns and permissions the ACL evaluator resolved for the authenticated
identity. If a resource is missing from this map, the user cannot access it.

### Interaction with Operations Level

ACL and [operations level](configuration.md#operations-level) are independent checks that _both_ must pass for a
write operation to succeed. Operations level acts as a global ceiling -- it controls which categories of write
operations are available to _anyone_. ACL controls which specific resources each user can modify.

| Scenario | Result |
|---|---|
| Operations level allows the action, ACL grants `write` | Allowed |
| Operations level allows the action, ACL denies `write` | Denied (`403 ACL002`) |
| Operations level blocks the action, ACL grants `write` | Denied (`403 OPS001`) |
| Operations level blocks the action, ACL denies `write` | Denied (`403 OPS001`) |

A common pattern is to use operations level as a coarse safety net and ACL for fine-grained per-user control:

- **`operations_level=1`** (operational) + ACL grants → users can scale, restart, and rollback the services they have
  `write` grants for, but nobody can modify service definitions or delete resources regardless of grants.
- **`operations_level=2`** (configuration) + ACL grants → platform team members with `write` on `service:*` can edit
  env vars, resources, and placement. Dangerous operations (node changes, deletions) are still blocked for everyone.
- **`operations_level=3`** (impactful) + ACL grants → full write capability, scoped per user by ACL. Use this only
  when ACL policies are well-tested.

Operations level is set once at startup and applies uniformly. ACL policies can be hot-reloaded without restart.

### Allow Header

Every GET and HEAD response includes an `Allow` header listing the HTTP methods available for that resource. The header reflects both the configured operations level and the user's ACL write permission:

```
Allow: GET, HEAD, PUT, POST, PATCH
```

A read-only user (or one without write grants for the resource) sees only:

```
Allow: GET, HEAD
```

The frontend uses this header to show or hide action buttons.

### Whoami Permissions

When ACL is active, the `/auth/whoami` response includes a `permissions` field summarizing the user's effective access patterns:

```json
{
  "subject": "alice@example.com",
  "displayName": "Alice",
  "provider": "oidc",
  "permissions": {
    "service:webapp-*": ["read", "write"],
    "stack:monitoring": ["read"]
  }
}
```
