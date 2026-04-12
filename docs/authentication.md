---
title: Authentication
description: OIDC, Tailscale, mTLS client certificates, and trusted proxy header authentication.
category: guide
tags: [authentication, oidc, tailscale, mtls, headers, security]
---

# Authentication

Cetacean supports pluggable authentication with five modes. Authentication is optional: The default mode (`none`)
allows anonymous access. One mode is active at a time via `auth.mode` (see [Configuration](configuration.md)).

All authentication is identity-only (who you are). For per-resource access control,
see [Authorization](authorization.md).

## Quick Start

All auth settings can be passed as CLI flags, environment variables, or config file keys. See
[Configuration](configuration.md) for the full precedence rules. The examples below use CLI flags; equivalent env vars
and config file keys are listed in each provider's configuration table.

```bash
# No auth (default)
./cetacean

# OIDC (e.g., Keycloak, Auth0, Okta, Dex)
./cetacean \
  -auth-mode oidc \
  -auth-oidc-issuer https://idp.example.com \
  -auth-oidc-client-id cetacean \
  -auth-oidc-client-secret secret \
  -auth-oidc-redirect-url https://cetacean.example.com/auth/callback

# Tailscale (local daemon)
./cetacean -auth-mode tailscale

# mTLS client certificates
./cetacean \
  -auth-mode cert \
  -auth-cert-ca /path/to/ca.pem \
  -tls-cert /path/to/server.pem \
  -tls-key /path/to/server-key.pem

# Trusted proxy headers
./cetacean \
  -auth-mode headers \
  -auth-headers-subject X-Remote-User \
  -trusted-proxies 10.0.0.0/8
```

## Identity Model

Every provider produces the same identity structure, available at `GET /auth/whoami`. The `subject` is the unique
identifier (OIDC `sub`, Tailscale user ID, certificate CN/SPIFFE URI, or header value). `groups` are used for
[authorization](authorization.md) audience matching. How each field is populated depends on the provider — see the
sections below.

## Providers

### None (Default)

Anonymous access. All requests receive a static identity with `subject: "anonymous"`.

No configuration required — this is the default when `auth.mode` is unset. Use this when Cetacean is behind a VPN,
firewall, or reverse proxy that handles authentication externally.

---

### OIDC

[OpenID Connect](https://openid.net/developers/how-connect-works/) with authorization code flow for browsers and Bearer token validation for machines/scripts.

#### Configuration

See [OIDC configuration](configuration#oidc) for all parameters.

#### Browser Flow

Unauthenticated browser requests are redirected to `/auth/login`, which initiates the standard authorization code flow
with your IdP. After authentication, the callback exchanges the code for tokens, validates the ID token, sets a session
cookie, and redirects to the original URL.

```
Browser                        Cetacean                          IdP
  │                               │                               │
  ├── GET /services ─────────────►│                               │
  │                               ├── 302 /auth/login ───────────►│
  │◄──────────────────────────────┤                               │
  ├── GET /auth/login ───────────►│                               │
  │◄── 302 to IdP authorize ──────┤                               │
  ├── GET authorize ─────────────────────────────────────────────►│
  │                                                               │
  │◄── 302 /auth/callback?code=...&state=... ─────────────────────┤
  ├── GET /auth/callback ────────►│                               │
  │                               ├── Validate state, nonce       │
  │                               ├── Exchange code for tokens ──►│
  │                               │◄── ID token + access token ───┤
  │                               ├── Validate ID token           │
  │                               ├── Set session cookie          │
  │◄── 302 to original URL ───────┤                               │
  ├── GET /services ─────────────►│                               │
  │                               ├── Validate session cookie     │
  │◄── 200 JSON ──────────────────┤                               │
```

#### Machine Flow

For scripts and API clients, send an ID token in the `Authorization` header. The token is validated against the IdP's
JWKS endpoint on every request.

```http tab
GET /services HTTP/1.1
Authorization: Bearer eyJhbGci...
Accept: application/json
```

```bash tab
curl -H "Authorization: Bearer eyJhbGci..." \
     -H "Accept: application/json" \
     http://localhost:9000/services
```

#### Session Persistence

By default, the session signing key is generated randomly at startup — restarting the server invalidates all browser
sessions. Set `auth.oidc.session_key` to a fixed value for persistence across restarts:

```bash
openssl rand -hex 32   # generate a 32-byte key
./cetacean -auth-oidc-session-key a1b2c3...
```

#### Logout

`POST /auth/logout` clears the session cookie. If the IdP supports it ([RFC 9722](https://www.rfc-editor.org/rfc/rfc9722)), the user is also redirected to the IdP for sign-out.

#### IdP Setup Examples

**[Keycloak](https://www.keycloak.org/):**

1. Create a client with `confidential` access type
2. Set valid redirect URI to `https://cetacean.example.com/auth/callback`
3. Enable "Standard Flow" (authorization code)
4. Note the client ID and secret from the Credentials tab

**[Auth0](https://auth0.com/):**

1. Create a "Regular Web Application"
2. Add `https://cetacean.example.com/auth/callback` to Allowed Callback URLs
3. Add `https://cetacean.example.com` to Allowed Logout URLs
4. Use the Auth0 domain as the issuer (e.g., `https://your-tenant.auth0.com`)

**[Dex](https://dexidp.io/):**

```yaml
staticClients:
  - id: cetacean
    secret: your-secret
    name: Cetacean
    redirectURIs:
      - https://cetacean.example.com/auth/callback
```

---

### Tailscale

Identifies users via the [Tailscale](https://tailscale.com/) WhoIs API. Every request from a tailnet peer is automatically authenticated -- no
login flow needed.

#### Choosing a Mode

Tailscale auth has two modes. Pick based on your deployment:

|                                  | Local mode (default)                                                                                    | tsnet mode                                                                                    |
|----------------------------------|---------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------|
| **How it works**                 | Queries the host's Tailscale daemon to identify peers                                                   | Embeds a Tailscale node inside the Cetacean process                                           |
| **Tailscale installed on host?** | Yes (daemon must be running)                                                                            | No                                                                                            |
| **Network binding**              | Listens on all interfaces (`server.listen_addr`); only Tailscale IPs are authenticated, others rejected | Authenticated routes listen exclusively on the tailnet; non-tailnet traffic cannot reach them |
| **Docker health checks**         | Work normally (health endpoint is auth-exempt)                                                          | Work normally — meta endpoints (`/-/health`, `/-/ready`) remain on the regular listener       |
| **Config complexity**            | Minimal: just `-auth-mode tailscale`                                                                    | Requires an auth key, hostname, and persistent state directory                                |
| **Best for**                     | Hosts already running Tailscale (bare-metal, VMs)                                                       | Containers, Docker Swarm services, or hosts without Tailscale installed                       |

**Security note on local mode:** Cetacean binds to `server.listen_addr` (default `:9000`, all interfaces). A
defense-in-depth IP range check rejects requests not from Tailscale's [CGNAT](https://www.rfc-editor.org/rfc/rfc6598) (`100.64.0.0/10`) or [ULA](https://www.rfc-editor.org/rfc/rfc4193)
(`fd7a:115c:a1e0::/48`) ranges, but this is an application-layer check, not a socket-level restriction. For tighter
isolation, bind to your node's Tailscale IP (e.g. `-listen-addr 100.x.x.x:9000`) or use tsnet mode, which only
accepts connections through the embedded Tailscale node.

#### Local Mode (Default)

Uses the local Tailscale daemon to identify peers. Cetacean must run on a node inside the tailnet.

```bash
./cetacean -auth-mode tailscale
```

Requires the Tailscale daemon running locally (access to `/run/tailscale/tailscaled.sock`).

#### tsnet Mode

Embeds a Tailscale node directly into Cetacean. No local Tailscale installation is needed.

```bash
./cetacean \
  -auth-mode tailscale \
  -auth-tailscale-mode tsnet \
  -auth-tailscale-authkey tskey-auth-... \
  -auth-tailscale-hostname cetacean \
  -auth-tailscale-state-dir /var/lib/cetacean/tsnet
```

In tsnet mode, authenticated routes are served on the tailnet listener. Meta-endpoints (`/-/health`, `/-/ready`)
remain on the regular listener for Docker health checks.

#### Configuration

See [Tailscale configuration](configuration#tailscale) for all parameters.

#### Capability-Based Groups

Tailscale ACL capabilities can map users to application groups. Set `auth.tailscale.capability`:

```bash
./cetacean -auth-mode tailscale -auth-tailscale-capability example.com/cap/cetacean
```

Then in your Tailscale ACL policy, grant capabilities to users or groups:

```json
{
  "grants": [
    {
      "src": [
        "group:admins"
      ],
      "dst": [
        "tag:cetacean"
      ],
      "app": {
        "example.com/cap/cetacean": [
          {
            "groups": [
              "admin",
              "operators"
            ]
          }
        ]
      }
    }
  ]
}
```

Multiple grants are deduplicated and merged into the identity's `groups` array.

---

### Client Certificates (mTLS)

Authenticates via [mTLS](https://en.wikipedia.org/wiki/Mutual_authentication#mTLS) client certificates. Supports standard [X.509](https://www.rfc-editor.org/rfc/rfc5280) certificates and SPIFFE X.509-SVIDs for
workload identity.

**Requires TLS termination at Cetacean** (not behind a TLS-terminating proxy).

#### Configuration

See [Client certificate configuration](configuration#client-certificates) for CA settings and [TLS settings](configuration#general-settings) for server certificate and key.

```bash
./cetacean \
  -auth-mode cert \
  -auth-cert-ca /etc/cetacean/ca.pem \
  -tls-cert /etc/cetacean/server.pem \
  -tls-key /etc/cetacean/server-key.pem
```

Clients without a valid certificate cannot connect.

Identity is extracted from the certificate: SPIFFE URI SAN (highest priority), then email SAN, then Common Name.
Groups come from Organizational Unit (OU) fields. [SPIFFE](https://spiffe.io/) X.509-SVIDs are supported for
workload identity.

---

### Trusted Proxy Headers

Reads identity from HTTP headers set by a trusted reverse proxy (nginx, Traefik, Envoy, etc.).

> **Important:** This mode trusts that the proxy sets headers correctly. You must configure at least one security
> mechanism to prevent clients from spoofing headers by bypassing the proxy.

#### Configuration

See [Trusted proxy header configuration](configuration#trusted-proxy-headers) for all parameters.

Header auth requires the general `trusted_proxies` setting (see [General Settings](configuration.md#general-settings)).

> **Note:** The headers-specific `auth.headers.trusted_proxies` option is deprecated and will be removed in v1.

#### Security

The `trusted_proxies` setting is required—it restricts which IPs can set identity headers. Supports individual IPs and
CIDR notation (`10.0.0.0/8`). For additional protection, configure a shared secret that the proxy must include with
every request:

```bash
./cetacean \
  -auth-mode headers \
  -auth-headers-subject X-Remote-User \
  -auth-headers-secret-header X-Proxy-Secret \
  -auth-headers-secret-value my-secret-value \
  -trusted-proxies 10.0.0.0/8
```

#### Proxy Configuration Examples

**[nginx](https://nginx.org/)** with OAuth2 Proxy:

```nginx
location / {
    auth_request /oauth2/auth;
    auth_request_set $user   $upstream_http_x_auth_request_user;
    auth_request_set $email  $upstream_http_x_auth_request_email;
    auth_request_set $groups $upstream_http_x_auth_request_groups;

    proxy_set_header X-Remote-User   $user;
    proxy_set_header X-Remote-Email  $email;
    proxy_set_header X-Remote-Groups $groups;
    proxy_set_header X-Proxy-Secret  "my-secret-value";

    proxy_pass http://cetacean:9000;
}
```

**[Traefik](https://traefik.io/)** with ForwardAuth:

```yaml
http:
  middlewares:
    auth:
      forwardAuth:
        address: "http://auth-server/verify"
        authResponseHeaders:
          - "X-Remote-User"
          - "X-Remote-Email"
          - "X-Remote-Groups"
  routers:
    cetacean:
      middlewares:
        - auth
      service: cetacean
  services:
    cetacean:
      loadBalancer:
        servers:
          - url: "http://cetacean:9000"
```

---

## TLS

TLS termination is available in any auth mode and required for cert mode (mTLS). Set `-tls-cert` and `-tls-key` to
enable HTTPS. See the [TLS settings](configuration.md#general-settings) in the configuration reference.

## Docker Compose Examples

### OIDC with Keycloak

```yaml
services:
  cetacean:
    image: cetacean:latest
    environment:
      CETACEAN_AUTH_MODE: oidc
      CETACEAN_AUTH_OIDC_ISSUER: https://keycloak.example.com/realms/myorg
      CETACEAN_AUTH_OIDC_CLIENT_ID: cetacean
      CETACEAN_AUTH_OIDC_CLIENT_SECRET_FILE: /run/secrets/oidc_secret
      CETACEAN_AUTH_OIDC_REDIRECT_URL: https://cetacean.example.com/auth/callback
    secrets:
      - oidc_secret
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    deploy:
      placement:
        constraints: [ node.role == manager ]

secrets:
  oidc_secret:
    external: true
```

### Tailscale (tsnet)

```yaml
services:
  cetacean:
    image: cetacean:latest
    environment:
      CETACEAN_AUTH_MODE: tailscale
      CETACEAN_AUTH_TAILSCALE_MODE: tsnet
      CETACEAN_AUTH_TAILSCALE_AUTHKEY_FILE: /run/secrets/ts_authkey
      CETACEAN_AUTH_TAILSCALE_HOSTNAME: cetacean
    secrets:
      - ts_authkey
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - tsnet-state:/var/lib/cetacean/tsnet
    deploy:
      placement:
        constraints: [ node.role == manager ]

secrets:
  ts_authkey:
    external: true

volumes:
  tsnet-state:
```

### Behind nginx with Header Auth

```yaml
services:
  nginx:
    image: nginx:alpine
    ports:
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
    deploy:
      placement:
        constraints: [ node.role == manager ]

  cetacean:
    image: cetacean:latest
    environment:
      CETACEAN_AUTH_MODE: headers
      CETACEAN_AUTH_HEADERS_SUBJECT: X-Remote-User
      CETACEAN_AUTH_HEADERS_NAME: X-Remote-Name
      CETACEAN_AUTH_HEADERS_EMAIL: X-Remote-Email
      CETACEAN_TRUSTED_PROXIES: "10.0.0.0/8"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    deploy:
      placement:
        constraints: [ node.role == manager ]
```

## Verifying Your Setup

Check the current identity with `GET /auth/whoami`:

```http tab
GET /auth/whoami HTTP/1.1
```

```bash tab
curl -s http://localhost:9000/auth/whoami | jq .
```

See the [API reference](/api) for response schema and auth endpoint details.

## Authorization

For per-resource access control — controlling which users can view or modify which resources —
see [Authorization](authorization.md).
