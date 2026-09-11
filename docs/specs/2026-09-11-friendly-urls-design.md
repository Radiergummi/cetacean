# Friendly URLs: design

## Problem

A resource has exactly one URL today: `/services/<id>`. That is right for a
consumer holding an ID and wrong for everyone else. A person knows the service
as `shop_web`, or as `web` in the `shop` stack, and nothing in the API accepted
either until `internal/api/canonical.go` landed — at which point
`/services/shop_web` began answering `307 Temporary Redirect` to the ID form.

That first step covered a single name in place of a single ID. It did not cover
the way resources are actually described to each other: the config a service
mounts, the services attached to a network, the service inside a stack. Those
relationships exist in the cache and are rendered on every detail page, but
there is no way to *address* one — so a link to "the `smtp-password` config that
`shop_web` uses" has to be built out of two IDs the reader does not have.

## Decision

One canonical URL per resource, and a grammar of friendly addresses that
redirect to it.

A friendly URL is a chain of `(collection, identifier)` **pairs** followed by an
optional **suffix**:

```
/stacks/shop/services/web/configs/smtp-password
└─ pair ──┘└─ pair ────┘└─ pair ───────────────┘

/services/shop_web/logs?tail=50
└─ pair ─────────┘└ suffix ┘
```

The **target** is the last pair. The response is a `307` to the target's
canonical URL with the suffix and query string carried over unchanged. Every
earlier pair is a **context**: it must resolve, and it must be related to the
pair that follows it.

That one rule produces every form we want, including the ones already shipped,
and it keeps the middleware out of the routing business — the suffix is passed
through for the target's own routes to accept or reject.

Depth is capped at **two pairs**, or **three when the first is a stack**. A stack
is the one context that is itself a grouping rather than a resource, so
`/stacks/shop/services/web/configs/smtp-password` needs the extra level to be
expressible at all.

### Why redirect rather than serve

Serving the resource under the friendly name would give it two URLs, and the
things that name a URL would then disagree: the `ETag`, the JSON-LD `@id`, the
`Link` headers, the history feed and the SSE event all say `/services/<id>`. A
redirect keeps them right and costs one hop. `307` is the only status that fits:
it preserves the method and the body, so a write is addressable by name too
(`PUT /stacks/shop/services/web/scale`), where `301` and `302` permit a client to
rewrite the method to `GET`, and `308` is cacheable indefinitely while a name can
be moved to another resource.

## Path grammar

Segment counts, with the parse each produces. `collection` is a known plural
segment; `sub` is anything else.

| Segments | Example | Parse |
|---|---|---|
| 1 | `/services` | collection listing — untouched |
| 2 | `/services/web` | one pair |
| 3 | `/services/web/env` | one pair + suffix `env` |
| 4 | `/services/web/configs/smtp` | two pairs |
| 5 | `/stacks/shop/services/web/logs` | two pairs + suffix `logs` |
| 6 | `/stacks/shop/services/web/configs/smtp` | three pairs |
| 7 | `/stacks/shop/services/web/configs/smtp/labels` | three pairs + suffix |

Pairs are consumed greedily but only while the next segment is a known
collection **and** an edge exists from the current type **and** an identifier
follows it. Everything from the first segment that fails those tests is suffix.
Three consequences worth stating, because each is a route that already exists:

- `/services/web/configs` stays the existing "configs this service uses"
  listing. `configs` is a known collection, but no identifier follows, so it
  cannot open a pair.
- `/services/web/env` stays the existing sub-resource. `env` is not a
  collection, so it is suffix.
- `/services/web/networks/foo` becomes a traversal, because `networks` is a
  collection, `foo` is an identifier, and `service → network` is an edge. No
  route is shadowed today because no registered sub-resource route takes a
  further path parameter — an invariant a test has to hold, since adding
  `GET /services/{id}/networks/{x}` later would silently lose to the grammar.

## The adjacency table

One declaration, and the only place an edge is defined. Each entry supplies the
candidate set for its `via` segment.

| From | Via | To | Edge source |
|---|---|---|---|
| service | `configs` | config | `Spec.TaskTemplate.ContainerSpec.Configs` |
| service | `secrets` | secret | `…ContainerSpec.Secrets` |
| service | `networks` | network | `cluster.ServiceAttachments` |
| service | `volumes` | volume | `…ContainerSpec.Mounts`, volume mounts only |
| service | `tasks` | task | `Cache.ListTasksByService` |
| config | `services` | service | `Cache.ServicesUsingConfig` |
| secret | `services` | service | `Cache.ServicesUsingSecret` |
| network | `services` | service | `Cache.ServicesUsingNetwork` |
| volume | `services` | service | `Cache.ServicesUsingVolume` |
| node | `tasks` | task | `Cache.ListTasksByNode` |
| task | `services` | service | `task.ServiceID` |
| task | `nodes` | node | `task.NodeID` |
| stack | `services` | service | `Cache.Stack.Services` |
| stack | `configs` | config | `Cache.Stack.Configs` |
| stack | `secrets` | secret | `Cache.Stack.Secrets` |
| stack | `networks` | network | `Cache.Stack.Networks` |
| stack | `volumes` | volume | `Cache.Stack.Volumes` |

Every `via` segment is plural, including `task → services` and `task → nodes`
where exactly one resource is reachable. A grammar with two pluralisation rules
is one nobody can predict.

`service → networks` must go through `cluster.ServiceAttachments`, not through
`Endpoint.VirtualIPs`. That distinction is not cosmetic: a `dnsrr` service has no
virtual IPs, and deriving attachment from them is the exact bug `CLAUDE.md`
records against the topology builders. The rule lives in `internal/cluster` and
this table consumes it.

### Names inside a stack

Within a stack context, a service may be addressed by its compose name or its
Docker name — `/stacks/shop/services/web` and `/stacks/shop/services/shop_web`
both resolve. The candidate set is `Stack.Services`, which holds full names, and
an identifier matches an entry if it equals the entry or equals it with the
`<stack>_` prefix removed. The compose name is the whole point of the form: it
is what the reader has in their `compose.yaml`.

## Resolution

Each pair resolves **within its context**, not globally:

1. The first pair resolves against the cache — `cache.Resolve*` for the five
   ID-keyed types, the name itself for volumes and stacks.
2. Every later pair resolves against the candidate set its edge supplies.

Resolving within the candidate set rather than globally-then-checking is what
makes `/stacks/shop/services/web` work at all, since `web` is not a service name
anywhere in the cluster. It also means the relationship is verified by
construction: there is no way to resolve a pair whose edge does not exist.

That verification is deliberate. A path that reads "the `smtp-password` config
used by `shop_web`" must not answer when `shop_web` does not use it — otherwise
the form degrades into decoration that dresses up any pair of identifiers, and a
link that should tell you a relationship is gone tells you nothing.

### Authorization

`read` is required on **every resource named in the path**, not only the target.
Each pair's existence, and the edge between them, is disclosed by a successful
redirect, so each has to be earned. Stack grants already reach their member
types through `acl.Evaluator`'s `impliedTypes`, so the stack forms compose
without a rule of their own.

A step that fails under the caller's grants is indistinguishable from a step
that does not exist — both answer `404`. This matters more here than on the flat
form: a flat name falls through to a handler that answers its own `404`, but a
traversal path is registered in no mux, so the middleware answers directly and
must not answer differently for "forbidden" than for "absent".

## Failure modes

| Condition | Status | Code |
|---|---|---|
| Success | 307 | — |
| Any pair resolves to nothing, or to something unreadable | 404 | `API015` |
| The edge does not hold between two named resources | 404 | `API015` |
| A name matches more than one readable resource | 409 | `API014` |
| Depth beyond the cap | — | falls through, unmatched |

`API015` ("Unresolvable Path") is one code with a precise detail rather than two
codes, because the reader's situation is the same — this path leads nowhere —
and only the reason differs: `"service shop_web does not use config smtp"` is
the useful half, not the code.

`API014` already exists and already lists every candidate ID. Because that list
is itself a disclosure, it is gated on a type-level read grant
(`acl.Evaluator.TypeGrants`), which an identity with no grant on the type fails
— it gets the `404` instead. This was a leak in the first implementation, fixed
in the same change as this spec.

A `307` carries no `ETag` and no `Cache-Control`: it is not a representation, and
`307` is uncacheable by default, which is correct for an address that names a
resource by a name that can move.

## HTML: which forms serve the dashboard

This is the one open decision, because it is the only part that costs frontend
work rather than backend work. The API behaviour above is the same either way.

**Recommended — serve the SPA for the two shallow human forms only:**

- `/{collection}/{name}` — `/services/shop_web`
- `/stacks/{stack}/services/{name}` — `/stacks/shop/services/web`

and redirect everything deeper, even for HTML. Those two are what a person types
or pastes into a chat. The dashboard gains two route shapes; nobody hand-writes
`/stacks/shop/services/web/configs/smtp-password` into an address bar, and a
`307` to the config's page is a perfectly good answer when they follow such a
link from elsewhere.

**As originally proposed — serve the SPA for every friendly form.** Achievable,
and cheaper than it first looks, but only one implementation of it is sane: a
single catch-all route whose component asks the server where the path points
(`fetch(path, { headers: { Accept: "application/json" }, redirect: "manual" })`
and read `Location`) and then renders the matching detail page. Enumerating a
route per edge — roughly seventeen, doubled by the stack depth — would put the
grammar in TypeScript as well as Go, which is the two-places-one-rule shape
`CLAUDE.md` keeps recording as the cause of real drift.

The recommendation is therefore to build the shallow forms now and keep the
catch-all in reserve: asking the server where a path points means adopting it
later costs one component and one request, with no grammar duplicated. There is
no SEO consideration either way — Cetacean is not indexed.

## Frontend

### Phase 1 — accept names

Nothing in the dashboard has to change for a name-addressed URL to work. A
detail route's parameter is handed straight to `api.service(param)`, and `fetch`
follows the `307` transparently, so `/services/shop_web` already renders. Phase
1 is therefore the API grammar plus two verifications:

- An SSE subscription through the redirect. `EventSource` follows redirects per
  spec, but the per-resource streams are the one consumer that opens a
  long-lived request through this middleware, and it is untested.
- `useSwarmQuery`'s optimistic updates key on resource ID. A name route must
  resolve to the ID once on mount and use the ID internally from there, or an
  SSE event will not match the row it should replace.

### Phase 2 — generate names

Switch link generation to names for the types whose names are stable and unique:
services, configs, secrets, networks, volumes, stacks. Add the
`/stacks/:stack/services/:name` route, which is the form that makes breadcrumbs
mean something.

**Nodes and tasks keep ID URLs.** A node hostname can collide — that is what
`API014` exists for — and can change; a task has no name of its own. Keeping them
on IDs means the dashboard never has to render a disambiguation page, which is
the only expensive piece of UI this design could otherwise require.

Docker's name charset (`[a-zA-Z0-9][a-zA-Z0-9_.-]*`) is URL-safe, so there is no
encoding work.

One accepted consequence: a name URL bookmarked today can point at a different
resource tomorrow, if a service is removed and recreated under the same name.
That is what addressing by name means, and it is why the canonical form stays
the ID.

## Discoverability

`docs/api.md` documents the grammar — it already documents the flat name form
under "Resource identifiers", and this extends that section.

`api/openapi.yaml` gets **no** new paths. Enumerating seventeen edges plus the
stack depth would add dozens of paths carrying no schema the canonical path does
not already describe, and no test requires a registered route to appear in the
spec (`TestEveryReadEndpointMatchesSpec` walks the spec into the router, not the
reverse). The grammar belongs in prose and in the `Link` headers.

Advertising traversal templates on detail responses — extending
`writeLinkTemplate` to emit `/services/{id}/configs/{name}` beside the existing
`/services/{id}` — is the natural self-describing move and fits the existing RFC
8288 usage, but it is optional and deferred.

## Testing

The adjacency table is the single source of truth, so the tests walk it rather
than restating it:

- Every declared edge resolves, from a fixture holding one resource of every
  type wired to every other. This is the `TestTypeGrantsAgreesWithCan` pattern
  `CLAUDE.md` records for exactly this hazard: a table and the resolvers that
  read it drifting apart silently.
- Every reverse edge declared as reverse resolves in that direction too.
- No registered mux route is shadowed by the grammar. Walk the router's patterns
  and assert none of them would parse as a pair chain.
- An edge that does not hold answers `404` `API015`, naming both resources.
- Every pair's ACL is enforced independently: a grant on the target but not the
  context must not redirect, and vice versa.
- Ambiguity answers `409` only with a type-level read grant.
- Depth beyond the cap is not matched.

Two e2e lanes through the real binary, since the middleware sits in the chain and
the unit tests exercise it through a test router: one stack-scoped service read,
one stack-scoped traversal to a config, both asserting the `Location` and the
followed response.

## Out of scope

- Plugins. `/plugins` is keyed by name and has no cross-references worth
  traversing.
- Depth beyond three pairs, and traversals that start at a task or node and
  continue past one hop.
- Renaming. Swarm service names are immutable; a node hostname change simply
  moves the friendly URL, and the ID form is unaffected.
- Canonical `<link rel="canonical">` tags in the SPA. Nothing indexes this.

[authorization]: authorization
[dashboard]: dashboard
