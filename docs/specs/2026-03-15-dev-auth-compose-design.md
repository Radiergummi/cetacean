# Dev Auth Compose — Design Spec

**Date:** 2026-03-15

## Goal

A self-contained Docker Compose setup that runs four cetacean instances simultaneously, one per auth mode (except tailscale), so developers can test all authentication flows locally without manual cert generation or external IdP accounts.

## Prerequisites

Add `127.0.0.1 dex` to `/etc/hosts`. This is required because the OIDC issuer URL must be identical from both the browser (redirect to Dex login) and the cetacean container (discovery + token exchange). Using the Docker service name `dex` as the hostname satisfies both: Docker DNS resolves it inside the network, and the hosts entry resolves it on the developer's machine.

## File Layout

```
dev/auth/
  compose.yaml          # All services
  dex.yaml              # Dex OIDC provider config
  Caddyfile.oidc        # Caddy TLS termination for OIDC
  Caddyfile.headers     # Caddy reverse proxy injecting auth headers
  Caddyfile.cert        # Caddy reverse proxy presenting mTLS client cert
```

## Port Map

| Port | Auth Mode | Request Flow |
|------|-----------|--------------|
| 9001 | none      | Browser → cetacean-none (direct) |
| 9002 | oidc      | Browser → caddy-oidc (HTTPS, `tls internal`) → cetacean-oidc → Dex |
| 9003 | headers   | Browser → caddy-headers (injects headers) → cetacean-headers |
| 9004 | cert      | Browser → caddy-cert (plain HTTP in, mTLS out) → cetacean-cert |
| 5556 | —         | Dex OIDC provider (browser redirects here for login) |

## Services

### cert-init

- **Image:** `alpine` with inline `apk add --no-cache openssl`
- **Purpose:** One-shot container that generates CA + server + client certs into a shared `certs` volume.
- **Generates:**
  - `ca.pem` / `ca-key.pem` — Certificate Authority
  - `server.pem` / `server-key.pem` — Server cert (CN=cetacean-cert, SAN=DNS:cetacean-cert) for cetacean's TLS listener
  - `client.pem` / `client-key.pem` — Client cert (CN=caddy-proxy) that Caddy presents to cetacean
- **Config:** EC P-256, 365-day validity
- **Lifecycle:** Exits after writing files. Downstream services use `depends_on: cert-init: condition: service_completed_successfully`.

### dex

- **Image:** `dexidp/dex`
- **Port:** 5556 → 5556 (published — browser redirects here for OIDC login)
- **Config (`dex.yaml`):**
  - Issuer: `http://dex:5556/dex`
  - Static client: id=`cetacean`, secret=`cetacean-dev-secret`, redirectURIs=`[https://localhost:9002/auth/callback]`
  - Static user: email=`dev@localhost`, password=`password` (bcrypt)
  - Storage: SQLite in-memory

### cetacean-none

- **Image:** `cetacean:latest`
- **Port:** 9001 → 9000
- **Environment:** `CETACEAN_AUTH_MODE=none`
- **Volumes:** Docker socket (read-only)

### caddy-oidc

- **Image:** `caddy:2`
- **Port:** 9002 → 9002
- **Config:** Mounts `Caddyfile.oidc`
- **Behavior:** Terminates TLS on `:9002` using Caddy's `tls internal` (auto-generated local CA cert). Proxies to cetacean-oidc:9000 over plain HTTP. This is required because cetacean's OIDC session cookies use the `__Host-` prefix and `Secure: true`, which browsers reject over plain HTTP.
- **Depends on:** cetacean-oidc

### cetacean-oidc

- **Image:** `cetacean:latest`
- **No published port** — only reachable via caddy-oidc
- **Environment:**
  - `CETACEAN_AUTH_MODE=oidc`
  - `CETACEAN_AUTH_OIDC_ISSUER=http://dex:5556/dex`
  - `CETACEAN_AUTH_OIDC_CLIENT_ID=cetacean`
  - `CETACEAN_AUTH_OIDC_CLIENT_SECRET=cetacean-dev-secret`
  - `CETACEAN_AUTH_OIDC_REDIRECT_URL=https://localhost:9002/auth/callback`
- **Volumes:** Docker socket (read-only)
- **Depends on:** dex

### cetacean-headers

- **Image:** `cetacean:latest`
- **No published port** — only reachable via caddy-headers
- **Environment:**
  - `CETACEAN_AUTH_MODE=headers`
  - `CETACEAN_AUTH_HEADERS_SUBJECT=X-Auth-User`
  - `CETACEAN_AUTH_HEADERS_NAME=X-Auth-Name`
  - `CETACEAN_AUTH_HEADERS_EMAIL=X-Auth-Email`
  - `CETACEAN_AUTH_HEADERS_GROUPS=X-Auth-Groups`
- **Volumes:** Docker socket (read-only)

### caddy-headers

- **Image:** `caddy:2`
- **Port:** 9003 → 9003
- **Config:** Mounts `Caddyfile.headers`
- **Behavior:** Injects `X-Auth-User: dev@localhost`, `X-Auth-Name: Dev User`, `X-Auth-Email: dev@localhost`, `X-Auth-Groups: admins,developers` on all proxied requests to cetacean-headers:9000.
- **Depends on:** cetacean-headers

### cetacean-cert

- **Image:** `cetacean:latest`
- **No published port** — only reachable via caddy-cert
- **Environment:**
  - `CETACEAN_AUTH_MODE=cert`
  - `CETACEAN_AUTH_CERT_CA=/certs/ca.pem`
  - `CETACEAN_TLS_CERT=/certs/server.pem`
  - `CETACEAN_TLS_KEY=/certs/server-key.pem`
- **Volumes:** Docker socket (read-only), `certs` volume
- **Depends on:** cert-init

### caddy-cert

- **Image:** `caddy:2`
- **Port:** 9004 → 9004
- **Config:** Mounts `Caddyfile.cert` and `certs` volume
- **Behavior:** Listens on plain HTTP :9004, proxies to `https://cetacean-cert:9000` using client cert (`client.pem`/`client-key.pem`) and trusting the generated CA.
- **Depends on:** cert-init, cetacean-cert

## Network

Single bridge network `auth-dev`. All services attached. No external network dependencies.

## Caddyfile.oidc

```caddyfile
{
    local_certs
}

:9002 {
    tls internal
    reverse_proxy cetacean-oidc:9000
}
```

Caddy's `tls internal` generates a locally-trusted certificate. Browsers will show a certificate warning on first visit (accept once).

## Caddyfile.headers

```caddyfile
:9003

reverse_proxy cetacean-headers:9000 {
    header_up X-Auth-User "dev@localhost"
    header_up X-Auth-Name "Dev User"
    header_up X-Auth-Email "dev@localhost"
    header_up X-Auth-Groups "admins,developers"
}
```

## Caddyfile.cert

```caddyfile
:9004

reverse_proxy https://cetacean-cert:9000 {
    transport http {
        tls_client_auth /certs/client.pem /certs/client-key.pem
        tls_trusted_ca_certs /certs/ca.pem
    }
}
```

## Usage

```bash
# Prerequisites: add Dex hostname (one-time)
echo "127.0.0.1 dex" | sudo tee -a /etc/hosts

# Build cetacean image first
docker build -t cetacean:latest .

# Start all auth scenarios
docker compose -f dev/auth/compose.yaml up

# Test each mode
open http://localhost:9001    # no auth
open https://localhost:9002   # OIDC (accept cert warning, login: dev@localhost / password)
open http://localhost:9003    # headers (auto-authenticated as Dev User)
open http://localhost:9004    # cert (Caddy authenticates via mTLS)

# Verify identity
curl http://localhost:9001/auth/whoami
curl -k https://localhost:9002/auth/whoami   # -k for self-signed cert
curl http://localhost:9003/auth/whoami
curl http://localhost:9004/auth/whoami
```

## Out of Scope

- Tailscale auth mode (requires real tailscale daemon/auth key)
- Prometheus/monitoring integration (use `compose.monitoring.yaml` separately)
- Production TLS termination patterns
