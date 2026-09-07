---
title: Authentication
description: Configure anonymous, OIDC, Tailscale, mTLS certificate, or trusted proxy header authentication.
category: guide
tags: [authentication, oidc, tailscale, mtls, headers, security]
---

# Authentication

Cetacean authenticates requests with one of five providers, selected by `auth.mode`: `none`, `oidc`, `tailscale`,
`cert` or `headers`. One provider is active at a time. The default is `none`, which allows anonymous access.

Authentication establishes identity only. To control which resources an identity may view or change, see
[Authorization](authorization).

## Quick start

Every setting below is available as an environment variable and a config file key, and most also as a CLI flag. The examples use
flags; see [Configuration](configuration) for the full list and the precedence rules.

```bash
# No auth (default)
./cetacean

# OIDC (Keycloak, Auth0, Okta, Dex, ...)
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

## Identity

Every provider fills the same identity record. Read it with `GET /auth/whoami`. `subject` is the unique
identifier; `groups` feeds [authorization](authorization) audience matching.

| Field         | `none`      | `oidc`                             | `tailscale`             | `cert`                                | `headers`                      |
| ------------- | ----------- | ---------------------------------- | ----------------------- | ------------------------------------- | ------------------------------ |
| `subject`     | `anonymous` | `sub` claim                        | numeric user ID         | SPIFFE URI SAN, else CN, else email   | subject header                 |
| `displayName` | `Anonymous` | `name`, else `preferred_username`  | Tailscale display name  | CN, else the SPIFFE ID path           | name header, else subject      |
| `email`       | —           | `email` claim                      | Tailscale login name    | first email SAN                       | email header                   |
| `groups`      | —           | `groups` claim                     | app capability (below)  | Organizational Unit (OU) values       | groups header, comma-separated |

## Exempt paths

These paths skip authentication in every mode:

| Path                                              | Reason                                                   |
| ------------------------------------------------- | -------------------------------------------------------- |
| `/-/*`                                            | Health, readiness, metrics, SBOM and license endpoints   |
| `/api`, `/api/*`                                  | API documentation and the JSON-LD context                |
| `/assets/*`                                       | Dashboard static assets                                  |
| `/auth`, `/auth/*`                                | Login, callback, logout and `whoami`                     |
| `/mcp`                                            | The MCP server runs its own bearer-token check           |
| `/.well-known/*`                                  | OAuth discovery documents, unauthenticated by spec       |
| `/oauth/token`, `/oauth/revoke`, `/oauth/register` | Carry their own credentials in the request body          |

`/oauth/authorize` is not exempt: a user must authenticate before granting an MCP client access.

## None

Anonymous access. Every request receives a static identity with `subject: anonymous`.

This mode also bypasses [authorization](authorization): a configured ACL policy has no effect while `auth.mode` is
`none`. Use it when Cetacean sits behind a VPN, a firewall, or a proxy that authenticates for you.

## OIDC

[OpenID Connect](https://openid.net/developers/how-connect-works/) with the authorization code flow for browsers and
ID token validation for scripts. Requires `auth.oidc.issuer`, `auth.oidc.client_id`, `auth.oidc.client_secret` and
`auth.oidc.redirect_url`; the redirect URL must use HTTPS unless it points at a loopback address. See
[OIDC configuration](configuration#oidc) for all parameters.

### Browser flow

An unauthenticated request that accepts HTML is redirected to the IdP's authorization endpoint, with state, nonce and
PKCE verifier held in short-lived cookies. `GET /auth/callback` validates state and nonce, exchanges the code,
verifies the ID token, sets a session cookie, and redirects to the originally requested URL. `GET /auth/login` starts
the same flow explicitly and honours a relative `?redirect=` path.

### Machine flow

Send an ID token as a Bearer token. It is verified against the IdP's JWKS endpoint on every request.

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

### Sessions

The session cookie is `__Host-cetacean_session`: HMAC-signed, `HttpOnly`, `Secure`, `SameSite=Lax`. It expires with
the ID token, capped at 8 hours. Browser sessions therefore require HTTPS.

The signing key is generated randomly at startup, so restarting the server invalidates every browser session. Set
`auth.oidc.session_key` to a hex-encoded 32-byte value to keep sessions across restarts:

```bash
openssl rand -hex 32   # generate a 32-byte key
./cetacean -auth-oidc-session-key a1b2c3...
```

The cookie stores subject, display name, email and groups, but no raw token claims. Grants read from an OIDC claim
(`acl.oidc_claim`) therefore reach Bearer-token requests only; for browser users, match on `group:` audiences in the
policy instead.

### Logout

`POST /auth/logout` clears the session cookie. If the IdP advertises an `end_session_endpoint`
([RFC 9722](https://www.rfc-editor.org/rfc/rfc9722)), the user is also redirected there for sign-out.

### IdP setup

**[Keycloak](https://www.keycloak.org/):**

1. Create a client with `confidential` access type
2. Set the valid redirect URI to `https://cetacean.example.com/auth/callback`
3. Enable "Standard Flow" (authorization code)
4. Take the client ID and secret from the Credentials tab

**[Auth0](https://auth0.com/):**

1. Create a "Regular Web Application"
2. Add `https://cetacean.example.com/auth/callback` to Allowed Callback URLs
3. Add `https://cetacean.example.com` to Allowed Logout URLs
4. Use the Auth0 domain as the issuer (for example `https://your-tenant.auth0.com`)

**[Dex](https://dexidp.io/):**

```yaml
staticClients:
  - id: cetacean
    secret: your-secret
    name: Cetacean
    redirectURIs:
      - https://cetacean.example.com/auth/callback
```

## Tailscale

Identifies users through the [Tailscale](https://tailscale.com/) WhoIs API. Requests from tailnet peers are
authenticated without a login flow. See [Tailscale configuration](configuration#tailscale) for all parameters.

### Choosing a mode

|                              | Local mode (default)                                                             | tsnet mode                                                                |
| ---------------------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| How it works                 | Queries the host's Tailscale daemon to identify peers                            | Embeds a Tailscale node inside the Cetacean process                       |
| Tailscale installed on host? | Yes, the daemon must be running                                                  | No                                                                        |
| Network binding              | Listens on `server.listen_addr`; only Tailscale addresses are authenticated      | Serves the app on port 443 of the tailnet node                            |
| Docker health checks         | Work normally, the health endpoint is auth-exempt                                | Work normally, `/-/health` and `/-/ready` stay on the regular listener    |
| Config complexity            | Minimal: `-auth-mode tailscale`                                                  | Needs an auth key, a hostname and a persistent state directory            |
| Best for                     | Hosts already running Tailscale (bare metal, VMs)                                | Containers, Swarm services, or hosts without Tailscale installed          |

> **Note:** In local mode Cetacean binds to `server.listen_addr` (default `:9000`, all interfaces). Requests from
> outside Tailscale's [CGNAT](https://www.rfc-editor.org/rfc/rfc6598) (`100.64.0.0/10`) and
> [ULA](https://www.rfc-editor.org/rfc/rfc4193) (`fd7a:115c:a1e0::/48`) ranges are rejected, but that is an
> application-layer check, not a socket restriction. For tighter isolation, bind to the node's Tailscale address
> (`-listen 100.x.x.x:9000`) or use tsnet mode.

### Local mode

Uses the local Tailscale daemon to identify peers, so Cetacean must run on a node inside the tailnet with access to
`/run/tailscale/tailscaled.sock`.

```bash
./cetacean -auth-mode tailscale
```

### tsnet mode

Embeds a Tailscale node in the Cetacean process. No local Tailscale installation is needed. The full app is served on
port 443 of the tailnet node; `/-/health` and `/-/ready` stay on `server.listen_addr` for Docker health checks.

```bash
./cetacean \
  -auth-mode tailscale \
  -auth-tailscale-mode tsnet \
  -auth-tailscale-authkey tskey-auth-... \
  -auth-tailscale-hostname cetacean \
  -auth-tailscale-state-dir /var/lib/cetacean/tsnet
```

### Groups from capabilities

Set `auth.tailscale.capability` to map Tailscale app capabilities to identity groups:

```bash
./cetacean -auth-mode tailscale -auth-tailscale-capability example.com/cap/cetacean
```

Then grant that capability in your Tailscale ACL policy:

```json
{
  "grants": [
    {
      "src": ["group:admins"],
      "dst": ["tag:cetacean"],
      "app": {
        "example.com/cap/cetacean": [
          {
            "groups": ["admin", "operators"]
          }
        ]
      }
    }
  ]
}
```

Groups from multiple matching grants are merged and deduplicated. Malformed capability values are skipped.

## Client certificates (mTLS)

Authenticates with [mTLS](https://en.wikipedia.org/wiki/Mutual_authentication#mTLS) client certificates. Standard
[X.509](https://www.rfc-editor.org/rfc/rfc5280) certificates and [SPIFFE](https://spiffe.io/) X.509-SVIDs both work.
Requires TLS termination at Cetacean, so this mode cannot sit behind a TLS-terminating proxy. See
[client certificate configuration](configuration#client-certificates) for the CA setting.

```bash
./cetacean \
  -auth-mode cert \
  -auth-cert-ca /etc/cetacean/ca.pem \
  -tls-cert /etc/cetacean/server.pem \
  -tls-key /etc/cetacean/server-key.pem
```

Clients without a certificate signed by that CA cannot connect. The subject is taken from the SPIFFE URI SAN if the
certificate has one, otherwise the Common Name, otherwise the first email SAN; a certificate with none of the three is
rejected. Groups come from Organizational Unit (OU) fields. A certificate carrying more than one SPIFFE URI SAN is
rejected, since X.509-SVID allows exactly one.

## Trusted proxy headers

Reads identity from HTTP headers set by a reverse proxy (nginx, Traefik, Envoy). `auth.headers.subject` names the
header holding the subject and is required; the value must be non-empty, free of control characters, and at most 256
bytes. See [trusted proxy header configuration](configuration#trusted-proxy-headers) for the optional name, email and
groups headers.

> **Important:** This mode trusts the proxy to set headers correctly. `server.trusted_proxies` is required and
> restricts which source addresses may set identity headers, accepting individual IPs and CIDRs. Without it Cetacean
> refuses to start.

For defence in depth, require a shared secret on every proxied request:

```bash
./cetacean \
  -auth-mode headers \
  -auth-headers-subject X-Remote-User \
  -auth-headers-secret-header X-Proxy-Secret \
  -auth-headers-secret-value my-secret-value \
  -trusted-proxies 10.0.0.0/8
```

> **Note:** `auth.headers.trusted_proxies` is deprecated. Use `server.trusted_proxies`, which takes precedence when
> both are set.

### Proxy examples

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

## TLS

TLS termination works in any auth mode and is required for `cert` mode. Set `tls.cert` and `tls.key` to enable HTTPS.

## Deployment examples

Secret settings also accept a `_FILE` env var variant, which reads the value from a file. That is how the Swarm
examples below pass secrets.

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
        constraints: [node.role == manager]

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
      # Without this tsnet picks its own directory and the volume below
      # goes unused, so the node re-authenticates on every restart.
      CETACEAN_AUTH_TAILSCALE_STATE_DIR: /var/lib/cetacean/tsnet
    secrets:
      - ts_authkey
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - tsnet-state:/var/lib/cetacean/tsnet
    deploy:
      placement:
        constraints: [node.role == manager]

secrets:
  ts_authkey:
    external: true

volumes:
  tsnet-state:
```

### Behind a proxy with header auth

```yaml
services:
  cetacean:
    image: cetacean:latest
    environment:
      CETACEAN_AUTH_MODE: headers
      CETACEAN_AUTH_HEADERS_SUBJECT: X-Remote-User
      CETACEAN_AUTH_HEADERS_EMAIL: X-Remote-Email
      CETACEAN_AUTH_HEADERS_GROUPS: X-Remote-Groups
      CETACEAN_TRUSTED_PROXIES: "10.0.0.0/8"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    deploy:
      placement:
        constraints: [node.role == manager]
```

## Verifying your setup

`GET /auth/whoami` returns the identity the active provider produced:

```http tab
GET /auth/whoami HTTP/1.1
```

```bash tab
curl -s http://localhost:9000/auth/whoami | jq .
```

`GET /profile` returns the same identity plus the effective ACL grants. See the [API reference](/api) for the
response schemas.
