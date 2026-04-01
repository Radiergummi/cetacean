# Configuration

Cetacean is configured through CLI flags, environment variables, or a TOML config file. When the same setting is
specified in multiple places, the precedence order is: **flag > env var > config file > default**.

Sensitive settings (secrets, keys) also accept a `_FILE` suffix on their env var: set
`CETACEAN_AUTH_OIDC_CLIENT_SECRET_FILE=/run/secrets/oidc_secret` and Cetacean reads the secret from that file at
startup. This is the standard pattern for Docker Swarm secrets. The `_FILE` variant has lower precedence than the direct
env var. Trailing newlines are trimmed.

Supported `_FILE` variants: `CETACEAN_AUTH_OIDC_CLIENT_SECRET_FILE`, `CETACEAN_AUTH_OIDC_SESSION_KEY_FILE`,
`CETACEAN_AUTH_TAILSCALE_AUTHKEY_FILE`, `CETACEAN_AUTH_HEADERS_SECRET_VALUE_FILE`.

## General Settings

| Flag              | Env var                       | Config file key             | Default                       | Description                                                                    |
|-------------------|-------------------------------|-----------------------------|-------------------------------|--------------------------------------------------------------------------------|
| `-listen`         | `CETACEAN_LISTEN_ADDR`        | `server.listen_addr`        | `:9000`                       | HTTP server bind address                                                       |
| `-base-path`      | `CETACEAN_BASE_PATH`          | `server.base_path`          | _—_                           | URL base path prefix for sub-path deployments (e.g., `/cetacean`)              |
| `-docker-host`    | `CETACEAN_DOCKER_HOST`        | `docker.host`               | `unix:///var/run/docker.sock` | Docker socket URI                                                              |
| `-prometheus-url` | `CETACEAN_PROMETHEUS_URL`     | `prometheus.url`            | _—_                           | Prometheus base URL. Unset = metrics disabled.                                 |
| `-log-level`      | `CETACEAN_LOG_LEVEL`          | `logging.level`             | `info`                        | `debug`, `info`, `warn`, `error`                                               |
| `-log-format`     | `CETACEAN_LOG_FORMAT`         | `logging.format`            | `json`                        | `json` or `text`                                                               |
| `-pprof`          | `CETACEAN_PPROF`              | `server.pprof`              | `false`                       | Expose Go pprof endpoints at `/debug/pprof/`                                   |
| `-self-metrics`   | `CETACEAN_SELF_METRICS`       | `server.self_metrics`       | `true`                        | Expose Prometheus metrics at `/-/metrics`                                       |
| `-recommendations`| `CETACEAN_RECOMMENDATIONS`    | `server.recommendations`    | `true`                        | Enable recommendation engine                                                    |
| _—_               | `CETACEAN_OPERATIONS_LEVEL`   | `server.operations_level`   | `1`                           | Write operation tier: 0=read-only, 1=operational, 2=configuration, 3=impactful |
| _—_               | `CETACEAN_SSE_BATCH_INTERVAL` | `server.sse.batch_interval` | `100ms`                       | SSE event batching window (Go duration)                                        |
| _—_               | `CETACEAN_CORS_ORIGINS`       | `server.cors.origins`       | _—_                           | Allowed CORS origins (comma-separated or `*`). Unset = CORS disabled.          |
| _—_               | `CETACEAN_SNAPSHOT`           | `storage.snapshot`          | `true`                        | Enable disk persistence of swarm state                                         |
| _—_               | `CETACEAN_DATA_DIR`           | `storage.data_dir`          | `./data`                      | Directory for snapshot file                                                    |
| `-tls-cert`       | `CETACEAN_TLS_CERT`           | `tls.cert`                  | _—_                           | Server certificate path (PEM)                                                  |
| `-tls-key`        | `CETACEAN_TLS_KEY`            | `tls.key`                   | _—_                           | Server private key path (PEM)                                                  |
| `-config`         | `CETACEAN_CONFIG`             | _—_                         | _—_                           | Path to TOML config file                                                       |
| `-version`        | _—_                           | _—_                         | _—_                           | Print version and exit                                                         |

TLS cert and key must be set together or not at all. Required for `cert` auth mode (mTLS), optional otherwise.

### Sub-Path Deployment

Set `CETACEAN_BASE_PATH` to serve Cetacean under a URL prefix, for example behind a reverse proxy that routes
`/cetacean/` to your Cetacean instance:

```bash
CETACEAN_BASE_PATH=/cetacean
```

Or in TOML:

```toml
[server]
base_path = "/cetacean"
```

The value is normalized (leading slash added, trailing slash removed). All API responses, SSE endpoints, auth cookies,
and the frontend router automatically adjust to the configured prefix. No frontend rebuild is needed — the base path is
injected at runtime via a `<base>` tag.

When deploying behind a reverse proxy that **preserves** the path prefix (e.g., forwards `/cetacean/nodes` as-is), set
`CETACEAN_BASE_PATH=/cetacean`. When the proxy **strips** the prefix before forwarding, leave `CETACEAN_BASE_PATH`
unset.

## CORS

By default, Cetacean does not set any CORS headers — the embedded SPA is served from the same origin and doesn't need
them. Enable CORS when external scripts or dashboards on other origins need to call the API.

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

Requests from origins not in the allow-list receive no CORS headers and are blocked by the browser's same-origin policy
as before.

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

Requires `-tls-cert` and `-tls-key` to be set (mTLS needs TLS termination at Cetacean).

### Trusted Proxy Headers

| Flag                            | Env var                                 | Config file key                | Required    | Default | Description                                  |
|---------------------------------|-----------------------------------------|--------------------------------|-------------|---------|----------------------------------------------|
| `-auth-headers-subject`         | `CETACEAN_AUTH_HEADERS_SUBJECT`         | `auth.headers.subject`         | Yes         | _—_     | Header name for subject                      |
| `-auth-headers-name`            | `CETACEAN_AUTH_HEADERS_NAME`            | `auth.headers.name`            | No          | _—_     | Header name for display name                 |
| `-auth-headers-email`           | `CETACEAN_AUTH_HEADERS_EMAIL`           | `auth.headers.email`           | No          | _—_     | Header name for email                        |
| `-auth-headers-groups`          | `CETACEAN_AUTH_HEADERS_GROUPS`          | `auth.headers.groups`          | No          | _—_     | Header name for groups (comma-separated)     |
| `-auth-headers-secret-header`   | `CETACEAN_AUTH_HEADERS_SECRET_HEADER`   | `auth.headers.secret_header`   | No          | _—_     | Header name for shared secret                |
| `-auth-headers-secret-value`    | `CETACEAN_AUTH_HEADERS_SECRET_VALUE`    | `auth.headers.secret_value`    | Conditional | _—_     | Secret value (required if secret header set) |
| `-auth-headers-trusted-proxies` | `CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES` | `auth.headers.trusted_proxies` | No          | _—_     | Comma-separated CIDR/IP allowlist            |

At least one of `secret_header`+`secret_value` or `trusted_proxies` must be configured.

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
# trusted_proxies = "10.0.0.0/8,192.168.1.1"
```

Only the active auth mode's section matters. You can leave the others commented out or absent entirely.

## Subcommands

```
cetacean                  Start the server
cetacean healthcheck      Exit 0 if ready, 1 otherwise (for Docker HEALTHCHECK)
```

## Snapshots

Cetacean saves all cached swarm state to `${data_dir}/snapshot.json` after every sync. On startup, it loads the snapshot
to serve stale-but-fast data while the live sync catches up. Secret data is never persisted.

Writes are atomic (write to `.tmp`, then rename) so a crash mid-write won't corrupt it.

## Health Checks

Two meta endpoints, exempt from authentication and content negotiation:

**Liveness** -- `GET /-/health`

Always returns 200. Includes version, commit hash, and build date.

```json
{ "status": "ok", "version": "1.2.3", "commit": "abc1234", "buildDate": "2026-03-15" }
```

**Readiness** -- `GET /-/ready`

Returns 200 after the first Docker sync completes. Returns 503 until then.

```json
{ "status": "ready" }
```

The default Docker Compose healthcheck uses `cetacean healthcheck`, which hits the readiness endpoint internally.

```dockerfile
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
  CMD ["cetacean", "healthcheck"]
```

## Server Timeouts

| Timeout | Value    | Why                            |
|---------|----------|--------------------------------|
| Read    | 5s       | Prevent slow-loris             |
| Write   | 0 (none) | SSE connections are long-lived |
| Idle    | 120s     | Clean up abandoned connections |

The write timeout is intentionally zero because SSE streams are open-ended. Individual request timeouts are applied
where needed (e.g., 30s for Prometheus proxy, 10s for instant queries).

## Graceful Shutdown

Send `SIGINT` or `SIGTERM` and Cetacean will:

1. Stop accepting new connections
2. Wait up to 5 seconds for in-flight requests to complete
3. Close all SSE connections
4. Exit

## Docker Compose

The default `compose.yaml` deploys Cetacean on a manager node with sensible resource limits:

```yaml
services:
  cetacean:
    image: cetacean:latest
    ports:
      - "9000:9000"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      CETACEAN_PROMETHEUS_URL: ${CETACEAN_PROMETHEUS_URL:-}
    networks:
      - monitoring
    deploy:
      placement:
        constraints:
          - node.role == manager
      resources:
        limits:
          cpus: "2.0"
          memory: 1G
        reservations:
          cpus: "0.5"
          memory: 256M
```

For monitoring, see the [separate monitoring stack](monitoring.md).

## Operations Level

The `operations_level` setting controls which write operations are available. Use this to restrict Cetacean to a
read-only dashboard or limit it to safe operational actions. The default is `1`. Requests to endpoints above the
configured level receive a `403 Forbidden` response.

The current level is exposed in the health endpoint (`GET /-/health`) as `operationsLevel`, which the frontend uses to
hide disabled action buttons.

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
| Update healthcheck             | `PUT\|PATCH /services/{id}/healthcheck` |      —      |       —       |        ✔        |      ✔      |
| Update placement               | `PUT /services/{id}/placement`          |      —      |       —       |        ✔        |      ✔      |
| Patch ports                    | `PATCH /services/{id}/ports`            |      —      |       —       |        ✔        |      ✔      |
| Patch update policy            | `PATCH /services/{id}/update-policy`    |      —      |       —       |        ✔        |      ✔      |
| Patch rollback policy          | `PATCH /services/{id}/rollback-policy`  |      —      |       —       |        ✔        |      ✔      |
| Patch log driver               | `PATCH /services/{id}/log-driver`       |      —      |       —       |        ✔        |      ✔      |
| **Plugin management**          |                                         |             |               |                 |             |
| Enable/disable plugin          | `POST /plugins/{name}/enable\|disable`  |      —      |       —       |        ✔        |      ✔      |
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

## Recommendations

Cetacean continuously evaluates cluster health and surfaces recommendations on the dashboard, service list, and detail pages. Sizing thresholds are configurable; see [docs/recommendations.md](recommendations.md) for the full reference including all check categories, configuration options, and where recommendations appear.

## Profiling

When `pprof` is enabled, standard Go profiling endpoints are available at `/debug/pprof/`:

```bash
go tool pprof http://localhost:9000/debug/pprof/profile   # 30s CPU profile
go tool pprof http://localhost:9000/debug/pprof/heap       # heap snapshot
```
