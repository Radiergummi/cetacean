# One public URL: design

## Problem

Cetacean answers "what is my external URL?" in three unrelated places, and each
gets a different answer.

**1. The MCP issuer is hostless by default.** `main.go:690`:

```go
issuer := d.cfg.MCP.Issuer
if issuer == "" {
    scheme := "http"; if d.tlsEnabled { scheme = "https" }
    issuer = scheme + "://" + d.cfg.ListenAddr
}
```

`server.listen_addr` defaults to `:9000`, so the derived issuer is
`http://:9000` — a URL whose host is empty. `resolveMCPIssuer`
(`internal/config/mcp.go`) rejects exactly that shape when an operator types
it (`must include a host`), but the fallback path never reaches its validator.
The value feeds three things: the OAuth issuer identifier, the MCP resource
audience (`mcpResource := issuer + d.cfg.BasePath + "/mcp"`), and
`IconBaseURL` (`main.go:760`) — so it is wrong in every auth mode, not only
OIDC. This is why `docs/mcp.md` has to warn that getting `mcp.issuer` wrong
breaks sign-in: the default is not a weak guess, it is broken.

**2. `auth.oidc.redirect_url` is required but derivable.** The callback route
is fixed at `GET /auth/callback` (`internal/auth/oidc.go:171`), so the value is
mechanically `{external base}{base path}/auth/callback`. Worse,
`internal/auth/oidc.go:77` reverse-engineers the origin back out of it to build
the post-logout redirect — the code already treats `redirect_url` as a
smuggled public-URL setting.

**3. Feed links trust the client.** `absURL` (`internal/api/basepath.go:36`)
builds absolute URLs from `X-Forwarded-Proto` / `X-Forwarded-Host` without
consulting `server.trusted_proxies`, so any client can dictate them. And
`internal/api/feed_handlers.go:390` builds the Atom `tag:` URI from raw
`r.Host`, which makes an RFC 4151 identifier — defined to be permanent — vary
with the hostname a reader happened to use.

## Decision

Add `server.public_url` (`CETACEAN_PUBLIC_URL`, `-public-url`): the canonical
external URL clients reach this deployment at.

**Origin only.** Scheme, host and optional port — no path, query or fragment.
`server.base_path` already carries the external path prefix in both
directions: `basePathMiddleware` strips it from inbound requests and `absPath`
prepends it to outbound links, which means Cetacean already requires the proxy
to forward the prefix rather than strip it. A path in `public_url` is
rejected with an error naming `server.base_path`, so one concept keeps one
setting.

Consumers, each keeping its existing explicit setting as an override:

| Consumer | Today | With `public_url` |
|---|---|---|
| `mcp.issuer` | `scheme://listen_addr` | `public_url` |
| `auth.oidc.redirect_url` | required | `public_url + base_path + /auth/callback` |
| `absURL` | `X-Forwarded-*`, untrusted | `public_url + base_path + path` |
| Atom `tag:` URI | `r.Host` | `public_url`'s host |

Precedence everywhere: explicit setting > `public_url` > existing fallback.

Because `public_url` is origin-only it has the same shape `mcp.issuer` already
expects, so `mcpResource` and `IconBaseURL` keep appending `BasePath`
themselves and neither changes.

**Not CORS.** `server.cors.origins` lists *other* origins allowed to call
Cetacean — `internal/api/cors.go` matches the request's `Origin` header
against it. Cetacean's own origin never belongs there, so that setting stays
independent.

## Failing on a hostless issuer

When no issuer can be derived, what happens depends on whether OAuth is in
play:

- **`auth.mode != "none"`** — fail startup. OAuth cannot work: clients fetch
  `.well-known` documents from the issuer.
- **`auth.mode == "none"`** — warn. No OAuth server is constructed
  (`main.go:699`), so the only casualty is `IconBaseURL`. Failing here would
  break deployments that work today.

## Non-goals

- Deriving `server.base_path` from `public_url`, or vice versa. They answer
  different questions and the proxy topology decides both.
- Validating `X-Forwarded-*` against `server.trusted_proxies`. Setting
  `public_url` removes the reliance rather than hardening it; hardening the
  fallback is separate work.
