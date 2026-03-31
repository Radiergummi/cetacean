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

When using `CETACEAN_ACL_POLICY_FILE`, changes to the file are detected automatically:

- Valid new policy: swapped atomically, logged
- Invalid new policy: error logged, previous policy kept
- Inline env var (`CETACEAN_ACL_POLICY`) requires a restart to change

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
