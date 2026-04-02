# Configuration

Cetacean can be configured through CLI flags, environment variables, or a TOML config file. When the same setting is
specified in multiple places, the precedence order is: **flag > env var > config file > default**.

Sensitive settings (secrets, keys) also accept a `_FILE` suffix on their env var to read the secret from that file at
startup. The `_FILE` variant has lower precedence than the direct env var.

## General Settings

| Flag                  | Env var                       | Config file key             | Default                       | Description                                                                           |
|-----------------------|-------------------------------|-----------------------------|-------------------------------|---------------------------------------------------------------------------------------|
| `-listen`             | `CETACEAN_LISTEN_ADDR`        | `server.listen_addr`        | `:9000`                       | HTTP server bind address                                                              |
| `-base-path`          | `CETACEAN_BASE_PATH`          | `server.base_path`          | _—_                           | URL base path prefix for sub-path deployments (e.g., `/cetacean`)                     |
| `-docker-host`        | `CETACEAN_DOCKER_HOST`        | `docker.host`               | `unix:///var/run/docker.sock` | Docker socket URI                                                                     |
| `-prometheus-url`     | `CETACEAN_PROMETHEUS_URL`     | `prometheus.url`            | _—_                           | Prometheus base URL. Unset = metrics disabled.                                        |
| `-log-level`          | `CETACEAN_LOG_LEVEL`          | `logging.level`             | `info`                        | `debug`, `info`, `warn`, `error`                                                      |
| `-log-format`         | `CETACEAN_LOG_FORMAT`         | `logging.format`            | `json`                        | `json` or `text`                                                                      |
| `-pprof`              | `CETACEAN_PPROF`              | `server.pprof`              | `false`                       | Expose Go pprof endpoints at `/debug/pprof/`                                          |
| `-self-metrics`       | `CETACEAN_SELF_METRICS`       | `server.self_metrics`       | `true`                        | Expose Prometheus metrics at `/-/metrics`                                             |
| `-recommendations`    | `CETACEAN_RECOMMENDATIONS`    | `server.recommendations`    | `true`                        | Enable recommendation engine                                                          |
| `-operations-level`   | `CETACEAN_OPERATIONS_LEVEL`   | `server.operations_level`   | `1`                           | Write operation tier: 0=read-only, 1=operational, 2=configuration, 3=impactful        |
| `-sse-batch-interval` | `CETACEAN_SSE_BATCH_INTERVAL` | `server.sse.batch_interval` | `100ms`                       | SSE event batching window (Go duration)                                               |
| `-cors-origins`       | `CETACEAN_CORS_ORIGINS`       | `server.cors.origins`       | _—_                           | Allowed CORS origins (comma-separated or `*`). Unset = CORS disabled.                 |
| `-trusted-proxies`    | `CETACEAN_TRUSTED_PROXIES`    | `server.trusted_proxies`    | _—_                           | Trusted reverse proxy CIDRs/IPs (comma-separated). Enables real client IP resolution. |
| `-snapshot`           | `CETACEAN_SNAPSHOT`           | `storage.snapshot`          | `true`                        | Enable disk persistence of swarm state                                                |
| `-data-dir`           | `CETACEAN_DATA_DIR`           | `storage.data_dir`          | `./data`                      | Directory for snapshot file                                                           |
| `-tls-cert`           | `CETACEAN_TLS_CERT`           | `tls.cert`                  | _—_                           | Server certificate path (PEM)                                                         |
| `-tls-key`            | `CETACEAN_TLS_KEY`            | `tls.key`                   | _—_                           | Server private key path (PEM)                                                         |
| `-config`             | `CETACEAN_CONFIG`             | _—_                         | _—_                           | Path to TOML config file                                                              |

> **Note:** TLS cert and key must be set together or not at all. Required for `cert` auth mode (mTLS), optional
> otherwise.

### Sub-Path Deployment

Set `server.base_path` to serve Cetacean under a URL prefix, for example, behind a reverse proxy that routes
`/cetacean/` to your Cetacean instance:

```bash
CETACEAN_BASE_PATH=/cetacean
```

Or in TOML:

```toml
[server]
base_path = "/cetacean"
```

## CORS

By default, Cetacean does not set any CORS headers because it serves the embedded webapp from the same origin and
doesn’t need them. Enable CORS when external scripts or dashboards on other origins need to call the API.

```bash
# Allow specific origins
CETACEAN_CORS_ORIGINS=https://grafana.example.com,https://internal.example.com

# Allow any origin (not recommended with authentication)
CETACEAN_CORS_ORIGINS=*
```

Or in TOML:

```toml
[server.cors]
origins = ["https://grafana.example.com", "https://internal.example.com"]
```

When enabled, Cetacean handles `OPTIONS` preflight requests (responding with `204 No Content`) and adds the following
headers to cross-origin responses:

- `Access-Control-Allow-Origin` — the requesting origin (reflected from the allow-list)
- `Access-Control-Allow-Methods` — `GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS`
- `Access-Control-Allow-Headers` — `Accept, Authorization, Content-Type, If-None-Match, X-Request-ID`
- `Access-Control-Expose-Headers` — `ETag, Link, Allow, Location, Retry-After, X-Request-ID`
- `Access-Control-Max-Age` — `86400` (24 hours)

Requests from origins not in the allowlist receive no CORS headers and will be blocked by the browser’s same-origin
policy as before.

## Authentication and Authorization Settings

| Flag         | Env var              | Config file key | Default | Description                                                                                                                            |
|--------------|----------------------|-----------------|---------|----------------------------------------------------------------------------------------------------------------------------------------|
| `-auth-mode` | `CETACEAN_AUTH_MODE` | `auth.mode`     | `none`  | Auth provider: `none`, [`oidc`](#oidc), [`tailscale`](#tailscale), [`cert`](#client-certificates), [`headers`](#trusted-proxy-headers) |

### OIDC

| Flag                       | Env var                            | Config file key           | Required | Default                | Description                                               |
|----------------------------|------------------------------------|---------------------------|----------|------------------------|-----------------------------------------------------------|
| `-auth-oidc-issuer`        | `CETACEAN_AUTH_OIDC_ISSUER`        | `auth.oidc.issuer`        | Yes      | _—_                    | OIDC issuer URL                                           |
| `-auth-oidc-client-id`     | `CETACEAN_AUTH_OIDC_CLIENT_ID`     | `auth.oidc.client_id`     | Yes      | _—_                    | OAuth 2.0 client ID                                       |
| `-auth-oidc-client-secret` | `CETACEAN_AUTH_OIDC_CLIENT_SECRET` | `auth.oidc.client_secret` | Yes      | _—_                    | OAuth 2.0 client secret                                   |
| `-auth-oidc-redirect-url`  | `CETACEAN_AUTH_OIDC_REDIRECT_URL`  | `auth.oidc.redirect_url`  | Yes      | _—_                    | Callback URL (HTTPS required, loopback exempt)            |
| `-auth-oidc-scopes`        | `CETACEAN_AUTH_OIDC_SCOPES`        | `auth.oidc.scopes`        | No       | `openid,profile,email` | Comma-separated scopes                                    |
| `-auth-oidc-session-key`   | `CETACEAN_AUTH_OIDC_SESSION_KEY`   | `auth.oidc.session_key`   | No       | random                 | Hex-encoded 32-byte HMAC key; random per-process if unset |

### Tailscale

| Flag                         | Env var                              | Config file key             | Required   | Default    | Description                             |
|------------------------------|--------------------------------------|-----------------------------|------------|------------|-----------------------------------------|
| `-auth-tailscale-mode`       | `CETACEAN_AUTH_TAILSCALE_MODE`       | `auth.tailscale.mode`       | No         | `local`    | `local` or `tsnet`                      |
| `-auth-tailscale-authkey`    | `CETACEAN_AUTH_TAILSCALE_AUTHKEY`    | `auth.tailscale.authkey`    | tsnet only | _—_        | Auth key for node enrollment            |
| `-auth-tailscale-hostname`   | `CETACEAN_AUTH_TAILSCALE_HOSTNAME`   | `auth.tailscale.hostname`   | No         | `cetacean` | Tailscale node hostname                 |
| `-auth-tailscale-state-dir`  | `CETACEAN_AUTH_TAILSCALE_STATE_DIR`  | `auth.tailscale.state_dir`  | No         | _—_        | State directory for tsnet               |
| `-auth-tailscale-capability` | `CETACEAN_AUTH_TAILSCALE_CAPABILITY` | `auth.tailscale.capability` | No         | _—_        | App capability key for group extraction |

### Client Certificates

| Flag            | Env var                 | Config file key | Required | Default | Description             |
|-----------------|-------------------------|-----------------|----------|---------|-------------------------|
| `-auth-cert-ca` | `CETACEAN_AUTH_CERT_CA` | `auth.cert.ca`  | Yes      | _—_     | Path to CA bundle (PEM) |

> **Note:** Requires `-tls-cert` and `-tls-key` to be set (mTLS needs TLS termination at Cetacean).

### Trusted Proxy Headers

| Flag                            | Env var                                 | Config file key                | Required       | Default | Description                                  |
|---------------------------------|-----------------------------------------|--------------------------------|----------------|---------|----------------------------------------------|
| `-auth-headers-subject`         | `CETACEAN_AUTH_HEADERS_SUBJECT`         | `auth.headers.subject`         | Yes            | _—_     | Header name for subject                      |
| `-auth-headers-name`            | `CETACEAN_AUTH_HEADERS_NAME`            | `auth.headers.name`            | No             | _—_     | Header name for display name                 |
| `-auth-headers-email`           | `CETACEAN_AUTH_HEADERS_EMAIL`           | `auth.headers.email`           | No             | _—_     | Header name for email                        |
| `-auth-headers-groups`          | `CETACEAN_AUTH_HEADERS_GROUPS`          | `auth.headers.groups`          | No             | _—_     | Header name for groups (comma-separated)     |
| `-auth-headers-secret-header`   | `CETACEAN_AUTH_HEADERS_SECRET_HEADER`   | `auth.headers.secret_header`   | No             | _—_     | Header name for shared secret                |
| `-auth-headers-secret-value`    | `CETACEAN_AUTH_HEADERS_SECRET_VALUE`    | `auth.headers.secret_value`    | Conditional    | _—_     | Secret value (required if secret header set) |
| `-auth-headers-trusted-proxies` | `CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES` | `auth.headers.trusted_proxies` | **Deprecated** | _—_     | Use `-trusted-proxies` instead               |

> **Note:** `trusted_proxies` is always required. The shared secret is optional, additional protection.

See [Authentication](authentication.md) for detailed usage guides, flow diagrams, and deployment examples.

## Config File

Pass a TOML file via `-config` or `CETACEAN_CONFIG`:

```bash
cetacean -config /etc/cetacean/config.toml
```

The file uses nested TOML tables. Every field is optional, so you can omit what you don't need. Omitting a key means
_use the default,_ which is different from setting it to its zero value. For example, omitting `snapshot` defaults to
`true`, while `snapshot = false` explicitly disables it.

```toml
[server]
listen_addr = ":9000"
pprof = false
operations_level = 1
# trusted_proxies = "10.0.0.0/8"

[server.sse]
batch_interval = "100ms"

# [server.cors]
# origins = ["https://grafana.example.com"]

[docker]
host = "unix:///var/run/docker.sock"

[prometheus]
url = "http://prometheus:9090"

[logging]
level = "info"
format = "json"

[storage]
data_dir = "./data"
snapshot = true

[tls]
cert = "/etc/cetacean/server.pem"
key = "/etc/cetacean/server-key.pem"

[auth]
mode = "oidc"

[auth.oidc]
issuer = "https://idp.example.com"
client_id = "cetacean"
client_secret = "secret"
redirect_url = "https://cetacean.example.com/auth/callback"
scopes = "openid,profile,email"       # comma-separated
session_key = ""                       # hex-encoded 32 bytes; random if empty

# [auth.tailscale]
# mode = "local"                       # "local" or "tsnet"
# authkey = "tskey-auth-..."           # required for tsnet
# hostname = "cetacean"
# state_dir = "/var/lib/cetacean/tsnet"
# capability = "example.com/cap/cetacean"

# [auth.cert]
# ca = "/etc/cetacean/ca.pem"

# [auth.headers]
# subject = "X-Remote-User"
# name = "X-Remote-Name"
# email = "X-Remote-Email"
# groups = "X-Remote-Groups"
# secret_header = "X-Proxy-Secret"
# secret_value = "my-secret"
# trusted_proxies = "..."
```

Only the active auth mode’s section matters. You can leave the others commented out or absent entirely.

## Subcommands

```
cetacean                  Start the server
cetacean healthcheck      Exit 0 if ready, 1 otherwise (for Docker HEALTHCHECK)
```

## Snapshots

Cetacean saves all cached swarm state to `${data_dir}/snapshot.json` after every sync. On startup, it loads the snapshot
to serve stale-but-fast data while the live sync catches up. Writes are crash-safe.

## Health Checks

Two meta endpoints, exempt from authentication and content negotiation:

| Endpoint        | Behavior                                        | Use for                             |
|-----------------|-------------------------------------------------|-------------------------------------|
| `GET /-/health` | Always 200 if the process is running            | Uptime monitoring, restart policies |
| `GET /-/ready`  | 200 after the first Docker sync; 503 until then | Load balancers, `depends_on` gating |

The binary includes a `cetacean healthcheck` subcommand (used by the built-in Docker `HEALTHCHECK`).

## Operations Level

The `operations_level` setting controls which write operations are available. Use this to restrict Cetacean to a
read-only dashboard or limit it to safe operational actions. The default is `1`. Requests to endpoints above the
configured level receive a `403 Forbidden` response.

| Operation                      | Endpoint                                | 0 Read-only | 1 Operational | 2 Configuration | 3 Impactful |
|--------------------------------|-----------------------------------------|:-----------:|:-------------:|:---------------:|:-----------:|
| Browse all resources           | `GET /…`                                |      ✔      |       ✔       |        ✔        |      ✔      |
| **Reactive ops**               |                                         |             |               |                 |             |
| Scale service                  | `PUT /services/{id}/scale`              |      —      |       ✔       |        ✔        |      ✔      |
| Update service image           | `PUT /services/{id}/image`              |      —      |       ✔       |        ✔        |      ✔      |
| Rollback service               | `POST /services/{id}/rollback`          |      —      |       ✔       |        ✔        |      ✔      |
| Restart service                | `POST /services/{id}/restart`           |      —      |       ✔       |        ✔        |      ✔      |
| **Service definition changes** |                                         |             |               |                 |             |
| Patch environment variables    | `PATCH /services/{id}/env`              |      —      |       —       |        ✔        |      ✔      |
| Patch service labels           | `PATCH /services/{id}/labels`           |      —      |       —       |        ✔        |      ✔      |
| Patch configs                  | `PATCH /services/{id}/configs`          |      —      |       —       |        ✔        |      ✔      |
| Patch secrets                  | `PATCH /services/{id}/secrets`          |      —      |       —       |        ✔        |      ✔      |
| Patch networks                 | `PATCH /services/{id}/networks`         |      —      |       —       |        ✔        |      ✔      |
| Patch mounts                   | `PATCH /services/{id}/mounts`           |      —      |       —       |        ✔        |      ✔      |
| Patch container config         | `PATCH /services/{id}/container-config` |      —      |       —       |        ✔        |      ✔      |
| Patch resources                | `PATCH /services/{id}/resources`        |      —      |       —       |        ✔        |      ✔      |
| Replace healthcheck            | `PUT /services/{id}/healthcheck`        |      —      |       —       |        ✔        |      ✔      |
| Patch healthcheck              | `PATCH /services/{id}/healthcheck`      |      —      |       —       |        ✔        |      ✔      |
| Update placement               | `PUT /services/{id}/placement`          |      —      |       —       |        ✔        |      ✔      |
| Patch ports                    | `PATCH /services/{id}/ports`            |      —      |       —       |        ✔        |      ✔      |
| Patch update policy            | `PATCH /services/{id}/update-policy`    |      —      |       —       |        ✔        |      ✔      |
| Patch rollback policy          | `PATCH /services/{id}/rollback-policy`  |      —      |       —       |        ✔        |      ✔      |
| Patch log driver               | `PATCH /services/{id}/log-driver`       |      —      |       —       |        ✔        |      ✔      |
| **Plugin management**          |                                         |             |               |                 |             |
| Enable plugin                  | `POST /plugins/{name}/enable`           |      —      |       —       |        ✔        |      ✔      |
| Disable plugin                 | `POST /plugins/{name}/disable`          |      —      |       —       |        ✔        |      ✔      |
| Update plugin settings         | `PATCH /plugins/{name}/settings`        |      —      |       —       |        ✔        |      ✔      |
| **Resource creation**          |                                         |             |               |                 |             |
| Create config                  | `POST /configs`                         |      —      |       —       |        ✔        |      ✔      |
| Create secret                  | `POST /secrets`                         |      —      |       —       |        ✔        |      ✔      |
| Patch config labels            | `PATCH /configs/{id}/labels`            |      —      |       —       |        ✔        |      ✔      |
| Patch secret labels            | `PATCH /secrets/{id}/labels`            |      —      |       —       |        ✔        |      ✔      |
| **Dangerous operations**       |                                         |             |               |                 |             |
| Change node availability       | `PUT /nodes/{id}/availability`          |      —      |       —       |        —        |      ✔      |
| Patch node labels              | `PATCH /nodes/{id}/labels`              |      —      |       —       |        —        |      ✔      |
| Change service mode            | `PUT /services/{id}/mode`               |      —      |       —       |        —        |      ✔      |
| Change endpoint mode           | `PUT /services/{id}/endpoint-mode`      |      —      |       —       |        —        |      ✔      |
| Remove service                 | `DELETE /services/{id}`                 |      —      |       —       |        —        |      ✔      |
| Remove task                    | `DELETE /tasks/{id}`                    |      —      |       —       |        —        |      ✔      |
| Change node role               | `PUT /nodes/{id}/role`                  |      —      |       —       |        —        |      ✔      |
| Remove node                    | `DELETE /nodes/{id}`                    |      —      |       —       |        —        |      ✔      |
| Remove stack                   | `DELETE /stacks/{name}`                 |      —      |       —       |        —        |      ✔      |
| Remove config                  | `DELETE /configs/{id}`                  |      —      |       —       |        —        |      ✔      |
| Remove secret                  | `DELETE /secrets/{id}`                  |      —      |       —       |        —        |      ✔      |
| Remove network                 | `DELETE /networks/{id}`                 |      —      |       —       |        —        |      ✔      |
| Remove volume                  | `DELETE /volumes/{name}`                |      —      |       —       |        —        |      ✔      |
| Install plugin                 | `POST /plugins`                         |      —      |       —       |        —        |      ✔      |
| Upgrade plugin                 | `POST /plugins/{name}/upgrade`          |      —      |       —       |        —        |      ✔      |
| Remove plugin                  | `DELETE /plugins/{name}`                |      —      |       —       |        —        |      ✔      |
| **Swarm configuration**        |                                         |             |               |                 |             |
| Patch orchestration config     | `PATCH /swarm/orchestration`            |      —      |       —       |        ✔        |      ✔      |
| Patch Raft config              | `PATCH /swarm/raft`                     |      —      |       —       |        ✔        |      ✔      |
| Patch dispatcher config        | `PATCH /swarm/dispatcher`               |      —      |       —       |        ✔        |      ✔      |
| Patch CA config                | `PATCH /swarm/ca`                       |      —      |       —       |        —        |      ✔      |
| Toggle encryption              | `PATCH /swarm/encryption`               |      —      |       —       |        —        |      ✔      |
| Rotate join token              | `POST /swarm/rotate-token`              |      —      |       —       |        —        |      ✔      |
| Rotate unlock key              | `POST /swarm/rotate-unlock-key`         |      —      |       —       |        —        |      ✔      |
| Force CA rotation              | `POST /swarm/force-rotate-ca`           |      —      |       —       |        —        |      ✔      |
| Unlock swarm                   | `POST /swarm/unlock`                    |      —      |       —       |        —        |      ✔      |

### Interaction with ACL

When an [ACL policy](authorization.md) is active, operations level and ACL are checked independently: both must allow a
write for it to succeed. Operations level is a global ceiling (which _categories_ of writes exist at all),
while ACL controls _who_ can write to _which_ resources.

For example, with `operations_level=1` (operational):

- A user with `write` on `service:webapp` can scale and restart that service
- The same user cannot patch its environment variables (requires level 2), even though ACL allows it
- A user _without_ a `write` grant cannot scale any service, even though the level permits scaling

This lets you set a conservative operations level as a safety net while delegating fine-grained access through ACL
policy. See [Authorization — Interaction with Operations Level](authorization.md#interaction-with-operations-level)
for the full decision matrix.

## Recommendations

Cetacean continuously evaluates cluster health and surfaces recommendations on the dashboard, service list, and detail
pages. Sizing thresholds are configurable; see [docs/recommendations.md](recommendations.md) for the full reference
including all check categories, configuration options, and where recommendations appear.  
You can disable the recommendation engine entirely by setting the `recommendations` option to `false`.

## Profiling

When `pprof` is enabled, standard Go profiling endpoints are available at `/debug/pprof/`:

```bash
go tool pprof http://localhost:9000/debug/pprof/profile   # 30s CPU profile
go tool pprof http://localhost:9000/debug/pprof/heap       # heap snapshot
```
