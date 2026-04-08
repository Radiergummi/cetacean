---
title: Configuration
description: CLI flags, environment variables, TOML config file, operations level, health checks, and snapshots.
category: reference
tags: [configuration, environment, toml, operations-level]
---

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

### ACL

| Flag | Env var | Config file key | Default | Description |
|---|---|---|---|---|
| — | `CETACEAN_ACL_POLICY` | `acl.policy` | _—_ | Inline ACL policy (JSON, YAML, or TOML) |
| — | `CETACEAN_ACL_POLICY_FILE` | `acl.policy_file` | _—_ | Path to policy file (hot-reloaded on change) |
| — | `CETACEAN_ACL_LABELS` | `acl.labels` | `false` | Enable label-based ACL evaluation |

See [Authentication](authentication.md) for detailed usage guides, flow diagrams, and deployment examples.

## Config File

Pass a TOML file via `-config` or `CETACEAN_CONFIG`. Every field is optional — omitting a key uses the default.

```bash
cetacean -config /etc/cetacean/config.toml
```

See [`config.reference.toml`](./config.reference.toml) for a complete reference with all options, defaults, and
descriptions. Copy it as a starting point and uncomment what you need.

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

The `operations_level` setting controls which write operations are available. The default is `1`. Requests above the
configured level receive `403 Forbidden`. Each level includes everything below it.

| Operation                                                         | 0 Read-only | 1 Operational | 2 Configuration | 3 Impactful |
|-------------------------------------------------------------------|:-----------:|:-------------:|:---------------:|:-----------:|
| Browse all resources                                              |      ✔      |       ✔       |        ✔        |      ✔      |
| Scale, restart, rollback, update image                            |      —      |       ✔       |        ✔        |      ✔      |
| Edit service definitions (env, resources, ports, placement, etc.) |      —      |       —       |        ✔        |      ✔      |
| Create/edit configs, secrets, plugins                             |      —      |       —       |        ✔        |      ✔      |
| Edit swarm settings (raft, orchestration, dispatcher)             |      —      |       —       |        ✔        |      ✔      |
| Delete resources, change node availability/role                   |      —      |       —       |        —        |      ✔      |
| Change service/endpoint mode, remove nodes/stacks                 |      —      |       —       |        —        |      ✔      |
| Manage CA, encryption, rotate tokens, unlock swarm                |      —      |       —       |        —        |      ✔      |

When combined with [ACL](authorization.md#interaction-with-operations-level), both checks must pass — operations level
is the global ceiling, ACL controls per-user scope.

## Profiling

When `pprof` is enabled, standard Go profiling endpoints are available at `/debug/pprof/`:

```bash
go tool pprof http://localhost:9000/debug/pprof/profile   # 30s CPU profile
go tool pprof http://localhost:9000/debug/pprof/heap       # heap snapshot
```
