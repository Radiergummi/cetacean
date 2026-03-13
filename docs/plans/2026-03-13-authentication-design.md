# Authentication Design

## Goal

Add pluggable authentication to cetacean. One mode selected at startup via config. No credential storage — all modes delegate identity to external systems. Authorization (per-resource permissions) is out of scope.

## Auth Modes

| Mode | Identity source | Stateless? | Notes |
|------|----------------|------------|-------|
| `none` | Synthetic anonymous identity | Yes | Current behavior, default |
| `oidc` | OIDC provider (auth code + client credentials) | Cookie for browsers, Bearer for machines | Signed ephemeral cookie, no server-side sessions |
| `tailscale` | Tailscale `WhoIs` API | Yes | Local daemon or embedded tsnet |
| `cert` | TLS client certificate / SPIFFE | Yes | Requires TLS termination |
| `headers` | Trusted proxy headers | Yes | Optional shared secret validation |

## Core Types

### Identity

Populated by every provider and stored in request context.

```go
type Identity struct {
    Subject     string         // unique ID (OIDC sub, Tailscale user ID, cert CN/SPIFFE URI, header value)
    DisplayName string         // human-friendly name
    Email       string         // may be empty
    Groups      []string       // may be nil
    Provider    string         // "none", "oidc", "tailscale", "cert", "headers"
    Raw         map[string]any // all provider-specific claims
}
```

Context helpers: `IdentityFromContext(ctx)` and `ContextWithIdentity(ctx, identity)`.

### Provider Interface

```go
type Provider interface {
    // Authenticate extracts identity from the request.
    // May write a response directly (e.g., OIDC redirect) and return (nil, nil).
    // Error means authentication failed (→ 401).
    Authenticate(w http.ResponseWriter, r *http.Request) (*Identity, error)

    // RegisterRoutes adds provider-specific routes (e.g., /auth/callback).
    // No-op for most providers.
    RegisterRoutes(mux *http.ServeMux)
}
```

## Middleware

Inserted into the existing chain between `securityHeaders` and `negotiate`:

```
requestID → recovery → securityHeaders → auth → negotiate → discoveryLinks → requestLogger
```

### Route Exemptions

The middleware skips authentication for:
- `/-/*` — health, ready, metrics status
- `/api` — OpenAPI spec
- `/api/context.jsonld` — JSON-LD context
- `/assets/*` — static files
- `/auth/*` — OIDC callback and other auth routes

### Unauthenticated Responses

- **OIDC (browser/HTML)**: 302 redirect to `/auth/login`
- **OIDC (API/JSON)**: 401 with `WWW-Authenticate: Bearer`
- **All other modes**: 401
- **`none` mode**: never happens (always returns anonymous identity)

## Provider Details

### none

Returns a static anonymous `Identity` for every request. Subject: `anonymous`, Provider: `none`.

### oidc

Two code paths:

**Browser (auth code flow):**
1. Unauthenticated request → redirect to `/auth/login`
2. `/auth/login` stores CSRF state in a short-lived cookie, redirects to OIDC authorization endpoint
3. OIDC provider redirects to `/auth/callback` with code + state
4. Callback validates state, exchanges code for tokens, extracts ID token claims
5. Builds `Identity`, serializes to signed cookie (`cetacean_session`), redirects to original URL

**Machine (client credentials / Bearer token):**
1. Request includes `Authorization: Bearer <token>`
2. Token validated against OIDC provider's JWKS / introspection endpoint
3. Claims extracted into `Identity`

**Session cookie:**
- HMAC-SHA256 signed with ephemeral key generated at startup (sessions invalidate on restart)
- `HttpOnly`, `Secure` (configurable for dev), `SameSite=Lax`
- Expiry capped at 8 hours

### tailscale

**Local mode** (`CETACEAN_AUTH_TAILSCALE_MODE=local`):
- Calls `WhoIs` on local daemon socket with the request's remote address
- Non-Tailscale IPs → 401

**tsnet mode** (`CETACEAN_AUTH_TAILSCALE_MODE=tsnet`):
- Embeds a `tsnet.Server` with configured hostname and auth key
- Replaces the regular listener for all authenticated routes
- Regular listener still serves `/-/*` meta endpoints (for Docker health checks)
- Same `WhoIs` identity extraction

### cert

Unified client certificate and SPIFFE identity:
- `http.Server.TLSConfig` set to `tls.RequireAndVerifyClientCert` with configured CA bundle
- Identity extraction priority:
  1. SPIFFE URI SAN (`spiffe://...`) → subject
  2. Email SAN → email + subject
  3. CN → subject + display name
- Groups from OU fields
- All cert fields in `Raw`

Requires TLS termination (see TLS Configuration below).

### headers

- Reads identity from configured request headers
- Subject header is required; missing → 401
- Groups header split by comma
- Optional shared secret: if `CETACEAN_AUTH_HEADERS_SECRET_HEADER` is configured, the request must include that header with the expected value

## TLS Configuration

General TLS termination available in any mode:

```
CETACEAN_TLS_CERT=./server.pem
CETACEAN_TLS_KEY=./server-key.pem
```

If both are set, the server uses `ListenAndServeTLS`. Cert auth mode validates at startup that TLS is configured.

## Configuration

All auth config uses `CETACEAN_AUTH_` prefix (except TLS which uses `CETACEAN_TLS_`).

```
# Mode selection (default: none)
CETACEAN_AUTH_MODE=none|oidc|tailscale|cert|headers

# OIDC
CETACEAN_AUTH_OIDC_ISSUER=https://accounts.google.com
CETACEAN_AUTH_OIDC_CLIENT_ID=...
CETACEAN_AUTH_OIDC_CLIENT_SECRET=...
CETACEAN_AUTH_OIDC_REDIRECT_URL=https://cetacean.example.com/auth/callback
CETACEAN_AUTH_OIDC_SCOPES=openid,profile,email     # default: openid,profile,email

# Tailscale
CETACEAN_AUTH_TAILSCALE_MODE=local|tsnet             # default: local
CETACEAN_AUTH_TAILSCALE_AUTHKEY=tskey-...             # required for tsnet
CETACEAN_AUTH_TAILSCALE_HOSTNAME=cetacean             # tsnet hostname, default: cetacean
CETACEAN_AUTH_TAILSCALE_STATE_DIR=/var/lib/cetacean/tsnet

# Cert (requires CETACEAN_TLS_CERT and CETACEAN_TLS_KEY)
CETACEAN_AUTH_CERT_CA=./ca.pem

# Headers
CETACEAN_AUTH_HEADERS_SUBJECT=X-Forwarded-User       # required
CETACEAN_AUTH_HEADERS_NAME=X-Forwarded-Name           # optional
CETACEAN_AUTH_HEADERS_EMAIL=X-Forwarded-Email         # optional
CETACEAN_AUTH_HEADERS_GROUPS=X-Forwarded-Groups       # optional, comma-separated
CETACEAN_AUTH_HEADERS_SECRET_HEADER=X-Proxy-Secret    # optional
CETACEAN_AUTH_HEADERS_SECRET_VALUE=...                # required if secret_header set

# TLS (any mode)
CETACEAN_TLS_CERT=./server.pem
CETACEAN_TLS_KEY=./server-key.pem
```

Validation at startup: mode-specific required fields checked, invalid config → fatal error.

## Frontend Integration

**Identity endpoint**: `GET /auth/whoami` — returns the current `Identity` as JSON. Requires authentication (not under `/-/` exemption).

**SPA behavior:**
- On 401: OIDC mode → redirect to `/auth/login`; other modes → unauthenticated error state
- Display identity in nav bar (display name / email)
- Auth mode discoverable from `/auth/whoami` response

**SSE**: cookies sent automatically on EventSource reconnect. Cert/Tailscale/headers are per-connection and work transparently.

## Package Structure

```
internal/auth/
  identity.go       # Identity type, context helpers
  provider.go        # Provider interface
  middleware.go      # Auth middleware, route exemption logic
  session.go         # Signed cookie encode/decode (OIDC)
  none.go            # NoneProvider
  oidc.go            # OIDCProvider
  tailscale.go       # TailscaleProvider (local + tsnet)
  cert.go            # CertProvider
  headers.go         # HeadersProvider
```

**New dependencies:**
- `github.com/coreos/go-oidc/v3` — OIDC discovery and token validation
- `golang.org/x/oauth2` — auth code flow
- `tailscale.com/tsnet` + `tailscale.com/client/tailscale` — Tailscale integration

## Future Considerations (out of scope)

- **Authorization**: per-resource permissions for swarm entities
- **Mid-stream session expiry**: terminating active SSE connections when sessions expire
- **Token refresh**: OIDC refresh tokens to extend sessions without re-login
- **Audit logging**: who accessed what, when
- **Multi-provider**: combining auth modes simultaneously
