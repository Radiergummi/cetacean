# Authentication

Cetacean supports pluggable authentication with five providers. Authentication is optional: The default mode (`none`)
allows anonymous access. One provider is active at a time, selected via the `CETACEAN_AUTH_MODE` environment variable.

All authentication is identity-only (who you are). There is no authorization layer—every authenticated user has the
same read-only access to all swarm resources.

## Quick Start

```bash
# No auth (default)
CETACEAN_AUTH_MODE=none ./cetacean

# OIDC (e.g., Keycloak, Auth0, Okta, Dex)
CETACEAN_AUTH_MODE=oidc \
  CETACEAN_AUTH_OIDC_ISSUER=https://idp.example.com \
  CETACEAN_AUTH_OIDC_CLIENT_ID=cetacean \
  CETACEAN_AUTH_OIDC_CLIENT_SECRET=secret \
  CETACEAN_AUTH_OIDC_REDIRECT_URL=https://cetacean.example.com/auth/callback \
  ./cetacean

# Tailscale (local daemon)
CETACEAN_AUTH_MODE=tailscale ./cetacean

# mTLS client certificates
CETACEAN_AUTH_MODE=cert \
  CETACEAN_AUTH_CERT_CA=/path/to/ca.pem \
  CETACEAN_TLS_CERT=/path/to/server.pem \
  CETACEAN_TLS_KEY=/path/to/server-key.pem \
  ./cetacean

# Trusted proxy headers
CETACEAN_AUTH_MODE=headers \
  CETACEAN_AUTH_HEADERS_SUBJECT=X-Remote-User \
  CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES=10.0.0.0/8 \
  ./cetacean
```

## Identity Model

Every authenticated request produces an `Identity`:

```json
{
    "subject": "user-123",
    "displayName": "Alice",
    "email": "alice@example.com",
    "groups": [ "admin", "developers" ],
    "provider": "oidc",
    "raw": { "sub": "user-123", "email_verified": true }
}
```

| Field         | Type     | Description                                                                         |
|---------------|----------|-------------------------------------------------------------------------------------|
| `subject`     | string   | Unique identifier (OIDC `sub`, Tailscale user ID, cert CN/SPIFFE URI, header value) |
| `displayName` | string   | Human-friendly name                                                                 |
| `email`       | string   | Email address (if available)                                                        |
| `groups`      | string[] | Group memberships (if available)                                                    |
| `provider`    | string   | Provider name: `none`, `oidc`, `tailscale`, `cert`, `headers`                       |
| `raw`         | object   | Provider-specific claims (excluded from session cookies)                            |

The identity is available at `GET /auth/whoami` in all modes.

## Route Exemptions

The auth middleware skips these paths (accessible without authentication in all modes):

| Path        | Reason                                                               |
|-------------|----------------------------------------------------------------------|
| `/-/*`      | Meta endpoints (health, ready, metrics/status)                       |
| `/api*`     | API documentation (OpenAPI spec, JSON-LD context, Scalar playground) |
| `/assets/*` | Static frontend assets                                               |
| `/auth/*`   | Auth routes (login, callback, logout, whoami)                        |

All other routes require authentication when a non-`none` provider is active.

## Providers

### None (Default)

Anonymous access. All requests receive a static identity with `subject: "anonymous"`.

```bash
CETACEAN_AUTH_MODE=none  # or simply omit CETACEAN_AUTH_MODE
```

No configuration required. Use this when Cetacean is behind a VPN, firewall, or reverse proxy that handles
authentication externally.

---

### OIDC

OpenID Connect with authorization code flow for browsers and Bearer token validation for machines/scripts.

#### Configuration

| Variable                           | Config file key           | Required | Default                | Description                                                                    |
|------------------------------------|---------------------------|----------|------------------------|--------------------------------------------------------------------------------|
| `CETACEAN_AUTH_OIDC_ISSUER`        | `auth.oidc.issuer`        | Yes      | --                     | OIDC issuer URL (must support OIDC Discovery)                                  |
| `CETACEAN_AUTH_OIDC_CLIENT_ID`     | `auth.oidc.client_id`     | Yes      | --                     | OAuth 2.0 client ID                                                            |
| `CETACEAN_AUTH_OIDC_CLIENT_SECRET` | `auth.oidc.client_secret` | Yes      | --                     | OAuth 2.0 client secret                                                        |
| `CETACEAN_AUTH_OIDC_REDIRECT_URL`  | `auth.oidc.redirect_url`  | Yes      | --                     | Callback URL (must be HTTPS, or `http://localhost`/`http://127.0.0.1` for dev) |
| `CETACEAN_AUTH_OIDC_SCOPES`        | `auth.oidc.scopes`        | No       | `openid,profile,email` | Comma-separated OIDC scopes                                                    |
| `CETACEAN_AUTH_OIDC_SESSION_KEY`   | `auth.oidc.session_key`   | No       | random                 | Hex-encoded 32-byte HMAC key for session cookies. Random per-process if unset. |

#### Browser Flow (Authorization Code)

```
Browser                        Cetacean                       IdP
  │                               │                             │
  ├──GET /services───────────────►│                             │
  │                               ├─ 302 /auth/login ──────────►│
  │◄──────────────────────────────┤                             │
  ├──GET /auth/login─────────────►│                             │
  │                               ├─ Set cookies (state,        │
  │                               │   nonce, PKCE verifier,     │
  │                               │   redirect URL)             │
  │◄──302 to IdP authorize────────┤                             │
  ├──GET authorize───────────────────────────────────────────────►
  │                                                              │
  │◄──302 /auth/callback?code=...&state=...──────────────────────┤
  ├──GET /auth/callback──────────►│                             │
  │                               ├─ Validate state, nonce      │
  │                               ├─ Exchange code for tokens───►│
  │                               │◄─ ID token + access token───┤
  │                               ├─ Validate ID token          │
  │                               ├─ Set session cookie         │
  │◄──302 to original URL────────┤                             │
  ├──GET /services───────────────►│                             │
  │                               ├─ Validate session cookie    │
  │◄──200 JSON──────────────────┤                             │
```

1. Unauthenticated browser request → 302 redirect to `/auth/login`
2. `/auth/login` stores CSRF state, nonce, PKCE verifier, and redirect URL as short-lived cookies (5-minute TTL), then
   redirects to the IdP
3. IdP authenticates the user and redirects back to `/auth/callback`
4. Callback validates state (CSRF), nonce, and issuer (RFC 9207), exchanges the authorization code with PKCE, validates
   the ID token, creates a signed session cookie, and redirects to the original URL

The redirect URL from `/auth/login?redirect=/services` must be a relative path starting with `/`. Protocol-relative
URLs (`//...`) and backslash prefixes are rejected.

#### Machine Flow (Bearer Token)

Send an ID token in the `Authorization` header:

```bash
curl -H "Authorization: Bearer eyJhbGci..." \
     -H "Accept: application/json" \
     http://localhost:9000/services
```

The token is validated against the IdP's JWKS endpoint on every request (no local caching). Multi-audience tokens are
validated per OIDC Core Section 3.1.3.7 (`azp` must match client ID when multiple audiences are present).

When a Bearer-authenticated request fails, the response includes `WWW-Authenticate: Bearer`.

#### Session Cookies

Browser sessions use signed ephemeral cookies:

| Property | Value                                                          |
|----------|----------------------------------------------------------------|
| Name     | `cetacean_session`                                             |
| Signing  | HMAC-SHA256 with 32-byte key                                   |
| HttpOnly | Yes (no JavaScript access)                                     |
| Secure   | Yes (HTTPS only)                                               |
| SameSite | Lax                                                            |
| MaxAge   | min(ID token expiry, 8 hours)                                  |
| Content  | Subject, display name, email, groups, provider (no raw claims) |

If `CETACEAN_AUTH_OIDC_SESSION_KEY` is not set, the signing key is generated randomly at startup. Restarting the server
invalidates all sessions (users must re-authenticate).

Set `CETACEAN_AUTH_OIDC_SESSION_KEY` to a fixed 32-byte hex-encoded value for session persistence across restarts:

```bash
# Generate a key
openssl rand -hex 32

# Use it
CETACEAN_AUTH_OIDC_SESSION_KEY=a1b2c3...  # 64 hex characters
```

#### Logout

```bash
# Browser: POST (CSRF-protected)
curl -X POST http://localhost:9000/auth/logout

# Or simply navigate to POST /auth/logout in the UI
```

Logout clears the session cookie. If the IdP advertises an `end_session_endpoint` (RFC 9722), the user is redirected to
the IdP for single sign-out with `id_token_hint`.

The logout endpoint uses `Sec-Fetch-Site` / `Origin` validation to prevent cross-site logout attacks.

#### Auth Endpoints

| Method | Path                         | Description                                      |
|--------|------------------------------|--------------------------------------------------|
| GET    | `/auth/login?redirect={url}` | Initiate OIDC login flow                         |
| GET    | `/auth/callback`             | OAuth callback (IdP redirects here)              |
| POST   | `/auth/logout`               | Clear session, optionally redirect to IdP logout |
| GET    | `/auth/whoami`               | Current identity                                 |

#### IdP Setup Examples

**Keycloak:**

1. Create a client with "confidential" access type
2. Set valid redirect URI to `https://cetacean.example.com/auth/callback`
3. Enable "Standard Flow" (authorization code)
4. Note the client ID and secret from the Credentials tab

**Auth0:**

1. Create a "Regular Web Application"
2. Add `https://cetacean.example.com/auth/callback` to Allowed Callback URLs
3. Add `https://cetacean.example.com` to Allowed Logout URLs
4. Use the Auth0 domain as the issuer (e.g., `https://your-tenant.auth0.com`)

**Dex:**

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

Identifies users via the Tailscale WhoIs API. Every request from a tailnet peer is automatically authenticated -- no
login flow needed.

#### Local Mode (Default)

Uses the local Tailscale daemon to identify peers. Cetacean must run on a node inside the tailnet.

```bash
CETACEAN_AUTH_MODE=tailscale
# CETACEAN_AUTH_TAILSCALE_MODE=local  # default
```

Requires the Tailscale daemon running locally (access to `/run/tailscale/tailscaled.sock`).

#### tsnet Mode

Embeds a Tailscale node directly into Cetacean. No local Tailscale installation needed.

```bash
CETACEAN_AUTH_MODE=tailscale
CETACEAN_AUTH_TAILSCALE_MODE=tsnet
CETACEAN_AUTH_TAILSCALE_AUTHKEY=tskey-auth-...
CETACEAN_AUTH_TAILSCALE_HOSTNAME=cetacean       # optional, default: "cetacean"
CETACEAN_AUTH_TAILSCALE_STATE_DIR=/var/lib/cetacean/tsnet  # optional
```

In tsnet mode, authenticated routes are served on the tailnet listener. Meta endpoints (`/-/health`, `/-/ready`,
`/-/metrics/status`) remain on the regular listener for Docker health checks.

#### Configuration

| Variable                             | Config file key             | Required   | Default    | Description                             |
|--------------------------------------|-----------------------------|------------|------------|-----------------------------------------|
| `CETACEAN_AUTH_TAILSCALE_MODE`       | `auth.tailscale.mode`       | No         | `local`    | `local` or `tsnet`                      |
| `CETACEAN_AUTH_TAILSCALE_AUTHKEY`    | `auth.tailscale.authkey`    | tsnet only | --         | Tailscale auth key for node enrollment  |
| `CETACEAN_AUTH_TAILSCALE_HOSTNAME`   | `auth.tailscale.hostname`   | No         | `cetacean` | Tailscale node hostname (tsnet mode)    |
| `CETACEAN_AUTH_TAILSCALE_STATE_DIR`  | `auth.tailscale.state_dir`  | No         | --         | State directory for tsnet               |
| `CETACEAN_AUTH_TAILSCALE_CAPABILITY` | `auth.tailscale.capability` | No         | --         | App capability key for group extraction |

#### Identity Extraction

| Identity field | Source                                     |
|----------------|--------------------------------------------|
| Subject        | Tailscale user ID (numeric)                |
| DisplayName    | User display name                          |
| Email          | Login name (usually email)                 |
| Groups         | From app capability grants (if configured) |

#### Capability-Based Groups

Tailscale ACL capabilities can map users to application groups. Set `CETACEAN_AUTH_TAILSCALE_CAPABILITY` to the
capability key:

```bash
CETACEAN_AUTH_TAILSCALE_CAPABILITY=example.com/cap/cetacean
```

Then in your Tailscale ACL policy, grant capabilities to users or groups:

```json
{
    "grants": [
        {
            "src": [ "group:admins" ],
            "dst": [ "tag:cetacean" ],
            "app": {
                "example.com/cap/cetacean": [
                    {
                        "groups": [ "admin", "operators" ]
                    }
                ]
            }
        }
    ]
}
```

Multiple grants are deduplicated and merged into the identity's `groups` array.

#### Address Validation

As a defense-in-depth measure, the provider validates that the remote address is in Tailscale IP ranges before calling
WhoIs:

- IPv4: `100.64.0.0/10` (CGNAT)
- IPv6: `fd7a:115c:a1e0::/48` (ULA)

Requests from non-Tailscale IPs are rejected immediately.

---

### Client Certificates (mTLS)

Authenticates via mTLS client certificates. Supports standard X.509 certificates and SPIFFE X.509-SVIDs for workload
identity.

**Requires TLS termination at Cetacean** (not behind a TLS-terminating proxy).

#### Configuration

| Variable                | Config file key | Required | Default | Description                                        |
|-------------------------|-----------------|----------|---------|----------------------------------------------------|
| `CETACEAN_AUTH_CERT_CA` | `auth.cert.ca`  | Yes      | --      | Path to CA bundle (PEM) for client cert validation |
| `CETACEAN_TLS_CERT`     | `tls.cert`      | Yes      | --      | Server certificate (PEM)                           |
| `CETACEAN_TLS_KEY`      | `tls.key`       | Yes      | --      | Server private key (PEM)                           |

```bash
CETACEAN_AUTH_MODE=cert \
  CETACEAN_AUTH_CERT_CA=/etc/cetacean/ca.pem \
  CETACEAN_TLS_CERT=/etc/cetacean/server.pem \
  CETACEAN_TLS_KEY=/etc/cetacean/server-key.pem \
  ./cetacean
```

The server is configured with `tls.RequireAndVerifyClientCert` -- clients without a valid certificate cannot connect.

#### Identity Extraction

Identity fields are extracted from the client certificate in priority order:

1. **SPIFFE URI SAN** (highest priority, for workload identity)
    - Subject set to the full SPIFFE URI (e.g., `spiffe://example.com/service/web`)
    - DisplayName set to the path component
2. **Email SAN** (fallback)
    - First email address used as subject and display name
3. **Common Name (CN)** (fallback)
    - CN used as subject and display name

Groups are extracted from the certificate's Organizational Unit (OU) fields.

#### SPIFFE Support

[SPIFFE](https://spiffe.io/) X.509-SVIDs are validated per the SPIFFE specification:

- URI must start with `spiffe://`
- Trust domain: lowercase alphanumeric, `.`, `-`, `_` (max 255 chars)
- Path: must start with `/`, no empty segments, no `.` or `..` segments
- Max total length: 2048 bytes
- No query or fragment components

```bash
# Generate a SPIFFE-compatible client cert (using your SPIFFE CA)
# The URI SAN should be: spiffe://trust-domain/path/to/workload

# Example with curl
curl --cert client.pem --key client-key.pem \
     --cacert ca.pem \
     https://cetacean.example.com:9000/services
```

#### Raw Claims

The `raw` field in the identity includes certificate metadata:

```json
{
    "serial": "0a:1b:2c:3d",
    "issuer_cn": "My CA",
    "not_after": "2027-01-15T00:00:00Z",
    "spiffe_id": "spiffe://example.com/service/web"
}
```

---

### Trusted Proxy Headers

Reads identity from HTTP headers set by a trusted reverse proxy (nginx, Traefik, Envoy, etc.).

**Important:** This mode trusts that the proxy sets headers correctly. You must configure at least one security
mechanism to prevent clients from spoofing headers by bypassing the proxy.

#### Configuration

| Variable                                | Config file key                | Required    | Default | Description                                            |
|-----------------------------------------|--------------------------------|-------------|---------|--------------------------------------------------------|
| `CETACEAN_AUTH_HEADERS_SUBJECT`         | `auth.headers.subject`         | Yes         | --      | Header name for subject (e.g., `X-Remote-User`)        |
| `CETACEAN_AUTH_HEADERS_NAME`            | `auth.headers.name`            | No          | --      | Header name for display name                           |
| `CETACEAN_AUTH_HEADERS_EMAIL`           | `auth.headers.email`           | No          | --      | Header name for email                                  |
| `CETACEAN_AUTH_HEADERS_GROUPS`          | `auth.headers.groups`          | No          | --      | Header name for groups (comma-separated)               |
| `CETACEAN_AUTH_HEADERS_SECRET_HEADER`   | `auth.headers.secret_header`   | No          | --      | Header name for shared secret                          |
| `CETACEAN_AUTH_HEADERS_SECRET_VALUE`    | `auth.headers.secret_value`    | Conditional | --      | Shared secret value (required if secret header is set) |
| `CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES` | `auth.headers.trusted_proxies` | No          | --      | Comma-separated CIDR/IP allowlist                      |

At least one of `TRUSTED_PROXIES` or `SECRET_HEADER`+`SECRET_VALUE` must be configured.

#### Security Mechanisms

**IP Allowlist** -- only accept headers from known proxy IPs:

```bash
CETACEAN_AUTH_MODE=headers \
  CETACEAN_AUTH_HEADERS_SUBJECT=X-Remote-User \
  CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES=10.0.0.1,10.0.0.2,127.0.0.1
```

Supports individual IPs and CIDR notation (`10.0.0.0/8`). Bare IPs are treated as `/32` (IPv4) or `/128` (IPv6).

**Shared Secret** -- proxy proves its identity with a secret header:

```bash
CETACEAN_AUTH_MODE=headers \
  CETACEAN_AUTH_HEADERS_SUBJECT=X-Remote-User \
  CETACEAN_AUTH_HEADERS_SECRET_HEADER=X-Proxy-Secret \
  CETACEAN_AUTH_HEADERS_SECRET_VALUE=my-secret-value
```

The secret is validated using constant-time comparison (HMAC-based) to prevent timing attacks.

**Both** -- for maximum security, combine both mechanisms:

```bash
CETACEAN_AUTH_MODE=headers \
  CETACEAN_AUTH_HEADERS_SUBJECT=X-Remote-User \
  CETACEAN_AUTH_HEADERS_NAME=X-Remote-Name \
  CETACEAN_AUTH_HEADERS_EMAIL=X-Remote-Email \
  CETACEAN_AUTH_HEADERS_GROUPS=X-Remote-Groups \
  CETACEAN_AUTH_HEADERS_SECRET_HEADER=X-Proxy-Secret \
  CETACEAN_AUTH_HEADERS_SECRET_VALUE=my-secret-value \
  CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES=10.0.0.0/8
```

#### Proxy Configuration Examples

**nginx** with OAuth2 Proxy:

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

**Traefik** with ForwardAuth:

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

#### Subject Validation

The subject header value is validated:

- Must not be empty
- Max 256 characters
- No control characters

Missing or invalid subject → 401 Unauthorized.

---

## TLS

TLS termination is available in any auth mode. It is **required** for cert mode (mTLS).

| Variable            | Config file key | Required               | Default | Description                   |
|---------------------|-----------------|------------------------|---------|-------------------------------|
| `CETACEAN_TLS_CERT` | `tls.cert`      | No (Yes for cert mode) | --      | Server certificate path (PEM) |
| `CETACEAN_TLS_KEY`  | `tls.key`       | No (Yes for cert mode) | --      | Server private key path (PEM) |

```bash
# TLS with any auth mode
CETACEAN_TLS_CERT=/path/to/cert.pem \
CETACEAN_TLS_KEY=/path/to/key.pem \
./cetacean
```

When TLS is enabled, Cetacean listens on HTTPS. This is useful when:

- Using cert mode (required for mTLS)
- Running without a TLS-terminating proxy
- Ensuring session cookies are transmitted securely (OIDC cookies have `Secure` flag)

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
      CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES: "10.0.0.0/8"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    deploy:
      placement:
        constraints: [ node.role == manager ]
```

## API Usage

### Check Current Identity

```bash
curl -s http://localhost:9000/auth/whoami | jq .
```

Response:

```json
{
    "subject": "alice",
    "displayName": "Alice Smith",
    "email": "alice@example.com",
    "groups": [ "admin" ],
    "provider": "oidc"
}
```

The response includes `Cache-Control: no-store` to prevent identity caching.

### Unauthenticated Requests

Behavior depends on the auth mode and request type:

| Mode      | Browser (Accept: text/html)         | API (Accept: application/json)   |
|-----------|-------------------------------------|----------------------------------|
| none      | Always authenticated (anonymous)    | Always authenticated (anonymous) |
| oidc      | 302 redirect to `/auth/login`       | 401 + `WWW-Authenticate: Bearer` |
| tailscale | 401                                 | 401                              |
| cert      | TLS handshake fails (no valid cert) | TLS handshake fails              |
| headers   | 401                                 | 401                              |

### Frontend Integration

The Cetacean SPA automatically:

1. Fetches identity from `/auth/whoami` on page load
2. Displays the user badge in the nav bar (hidden in `none` mode)
3. On 401 with `WWW-Authenticate: Bearer` (OIDC), redirects to `/auth/login` with the current URL as the redirect target

## Security Properties

### Session Management

- Session cookies are signed with HMAC-SHA256
- Session TTL capped at 8 hours (or ID token expiry, whichever is shorter)
- Cookie flags: `HttpOnly`, `Secure`, `SameSite=Lax`
- By default, sessions are ephemeral -- server restart invalidates all sessions
- Set `CETACEAN_AUTH_OIDC_SESSION_KEY` for persistence across restarts

### CSRF Protection

- OIDC login flow uses random state parameter (128-bit entropy)
- PKCE (S256) prevents authorization code interception
- Logout endpoint uses `Sec-Fetch-Site` / `Origin` validation
- All security-critical comparisons use constant-time algorithms

### RFC Compliance

| RFC               | Feature                                                                 |
|-------------------|-------------------------------------------------------------------------|
| RFC 6750          | Bearer token authentication scheme                                      |
| RFC 9110          | `WWW-Authenticate` response header                                      |
| RFC 9207          | OIDC Authorization Server Issuer Identification (mix-up attack defense) |
| RFC 9722          | RP-Initiated Logout with `id_token_hint`                                |
| SPIFFE X.509-SVID | SPIFFE trust domain and path validation                                 |

### Defense in Depth

- Tailscale address validation before WhoIs API call
- Headers provider requires IP allowlist or shared secret (cannot be unprotected)
- OIDC validates issuer, state, nonce, and authorized party (`azp`)
- Redirect URLs restricted to relative paths (no protocol-relative or absolute URLs)
- Subject header values validated for length and control characters
