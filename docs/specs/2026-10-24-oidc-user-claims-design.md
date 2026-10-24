# Which OIDC claims a `user:` grant matches — design

## Problem

A `user:<pattern>` audience matches an identity's subject or its email. OIDC Core §5.7 says
`email` must not be used as a unique identifier for the end user: a provider can reassign an
address, and some let users change their own. Whoever holds a recycled `alice@corp.com` inherits
every grant written for Alice.

#464 stopped matching an email the provider marks `email_verified: false`. An absent claim still
keeps the email, deliberately: Entra ID never sends it, and dropping the email there would lock
out every deployment that names its users by address. That leaves a reassigned verified address,
and a provider that omits the claim without verifying.

## Options

1. **Keep matching on email** and leave the risk to the docs. No migration, no protection.
2. **Match `user:` on `sub` only in `oidc` mode**, with a separate `email:<pattern>` audience an
   operator opts into. Correct by default, but it breaks every policy that names users by email
   today, and `sub` is an opaque identifier most operators would have to look up per user.
3. **A setting naming the claims `user:` matches**, `acl.oidc_user_claims`, defaulting to
   `["sub", "email"]`.

## Decision

Option 3. Nothing changes for an existing deployment, and an operator who can name users by
subject or group gets §5.7's guarantee with one line. Option 2's breakage buys nothing that
setting `["sub"]` does not.

- **Allowed values are `sub` and `email`.** `preferred_username` is no better an identifier than
  `email` (§5.7 names both), and the identity does not carry it: a browser session keeps no raw
  claims, and a token minted from a session carries only subject, email, name and groups. A value
  the identity cannot answer would match nothing and look like a working setting. Anything else
  refuses startup, as does an empty list, which would disable every `user:` grant unannounced.
- **Applied when a grant is checked**, not when the identity is built. A session or refresh grant
  restored from storage still carries its email; the evaluator ignores it, so a change takes
  effect at the next restart instead of when the last session expires. The access fingerprint
  goes through the same match, so a cached response cannot outlive the change.
- **Scoped by `auth.mode`, not by `Identity.Provider`.** An OAuth token's identity reads
  `oauth`, yet in `oidc` mode its subject and email came from the OIDC provider. The setting is
  given to the evaluator only in `oidc` mode, where every identity is a session or a token
  minted from one.
- **Read at startup only.** It lives under `acl.*` but is configuration, not policy; the policy
  file watcher reloads grants, not this.

## Other providers

Unaffected. Tailscale's login name, a certificate's email SAN and a proxy's email header are each
asserted by a party the operator already trusts for identity, and none is subject to §5.7. Their
`user:` audiences keep matching subject and email.

## Not registered

OIDC Core §5.7 is not claimed in `internal/spec/registry/`. The default still matches email, so
the requirement is met only when an operator opts in; a registry entry would claim a guarantee
the shipped configuration does not give.
