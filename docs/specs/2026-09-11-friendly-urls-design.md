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

The cap stops pair consumption; it does not reject the request. A path with more
`(collection, identifier)`-shaped segments than the cap allows has the excess
treated as suffix and carried over — `/services/web/configs/smtp/labels/x`
redirects to `/configs/<id>/labels/x`, where the target's own routes decide
whether `labels/x` means anything. There is no "too deep" failure mode.

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
| 4 | `/services/web/configs/smtp`, `/services/web/tasks/3` | two pairs |
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
| service | `tasks` | task | `Cache.ListTasksByService`, by ID or slot |
| config | `services` | service | `Cache.ServicesUsingConfig` |
| secret | `services` | service | `Cache.ServicesUsingSecret` |
| network | `services` | service | `Cache.ServicesUsingNetwork` |
| volume | `services` | service | `Cache.ServicesUsingVolume` |
| node | `tasks` | task | `Cache.ListTasksByNode`, by ID |
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

An exact match wins over a prefix-stripped one, mirroring ID-over-name in
`cache.resolveIn`. The two can genuinely collide: `docker stack deploy` always
prefixes, but a service created by hand can carry the stack label under any
name, so a `shop` stack may hold both `web` and `shop_web`, where the identifier
`web` matches the first exactly and the second stripped. Without the precedence
rule that is an ambiguity report for a path that has an obvious reading.

### Addressing a task

A task is the one type with no identifier of its own beyond a 25-character ID,
and the one place the traversal form buys something a flat URL cannot: inside a
service's task set, `/services/web/tasks/3` names slot 3, and
`/nodes/worker-2/tasks/<id>` scopes a task to the node holding it.

Slot addressing needs a disambiguation rule, because a slot is not unique.
Swarm keeps a task record for every replica it has replaced, so slot 3 of any
service that has been updated — or that restarts in a loop — names several
records, all but one of them history. **The live task wins**, where live is
`cache.TaskIsLive` (`DesiredState` is neither `shutdown` nor `remove`), which is
already the shared predicate behind every replica count and both placement
views. A slot with no live task resolves to nothing rather than to its most
recent corpse: a caller addressing `tasks/3` means the replica running there,
and there isn't one.

Global services have no slots. Their tasks render as `<service>.<node>`, so the
identifier in the traversal form is the node ID or hostname, and an unplaced
global task is addressable only by its own ID.

That rule cannot be written here alone. `internal/mcp`'s `resolveTask` already
resolves `<service>.<slot>` for `describe`, `get_logs` and `remove_task`, and it
currently returns the **first** name match in `(slot, ID)` order — the dead
record, deterministically, whenever a slot has been replaced. So it is a live
defect on its own terms, and fixing it in place would leave the two transports
disagreeing about what `web.3` means, which is the failure
`internal/api/canonical.go` exists to have ended.

Task resolution therefore moves to `internal/cluster` — `ResolveTask(c
*cache.Cache, identifier string)` — which is where `CLAUDE.md`'s own reasoning
already puts it: the name is derived by `cluster.TaskName`, and the cache cannot
own a rule that depends on a package importing it. `internal/mcp`'s
`resolveTask` delegates, REST's traversal calls the same function, and the
live-task preference is applied once. It is worth doing whether or not the
traversal forms are ever built, because the MCP defect is shipped today.

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

The single-pair form has a shortcut the traversal forms must not inherit: an
identifier that is already the canonical ID falls through untouched, because the
request is already addressed at the canonical URL. A traversal is never at its
target's canonical URL however its identifiers are spelled, so it always
redirects — `/services/web/volumes/shop-data` redirects to `/volumes/shop-data`
even though a volume's name *is* its canonical identifier.

### Cost

The single-pair form is a map lookup, or a scan of one type on a name. A
traversal is more expensive in the reverse direction: `ServicesUsingConfig` and
its siblings scan every service to find the edge, so `/configs/x/services/y`
costs O(services) per request where `/services/<id>` costs nothing. That is
acceptable on a dashboard and it only runs on paths of four segments or more, so
the overwhelmingly common request is untouched — but it is worth knowing before
a dashboard starts generating the reverse form on every config page, which is
the one plausible way to make it hot.

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

`API015` ("Unresolvable Path") is one code with a precise detail rather than two
codes, because the reader's situation is the same — this path leads nowhere —
and only the reason differs: `"service shop_web does not use config smtp"` is
the useful half, not the code.

`API015` belongs to 2b and should not be registered before it: an error code in
the catalog that nothing can produce is a promise to a reader of
`/api/errors`. 2a's own failures are all covered by `API014` and the handlers'
existing 404s.

`API014` already exists and already lists every candidate ID. Because that list
is itself a disclosure, it is gated on a type-level read grant
(`acl.Evaluator.TypeGrants`), which an identity with no grant on the type fails
— it gets the `404` instead. This was a leak in the first implementation, fixed
in the same change as this spec.

A `307` carries no `ETag` and no `Cache-Control`: it is not a representation, and
`307` is uncacheable by default, which is correct for an address that names a
resource by a name that can move.

## HTML: which forms serve the dashboard

Decided: the two shallow human forms serve the dashboard, everything deeper
redirects. The API behaviour above is the same either way — this is the only
part that costs frontend work rather than backend work.

**Serve the SPA for the two shallow human forms:**

- `/{collection}/{name}` — `/services/shop_web`
- `/stacks/{stack}/services/{name}` — `/stacks/shop/services/web`

and redirect everything deeper, even for HTML. Those two are what a person types
or pastes into a chat. The dashboard gains two route shapes; nobody hand-writes
`/stacks/shop/services/web/configs/smtp-password` into an address bar, and a
`307` to the config's page is a perfectly good answer when they follow such a
link from elsewhere.

**The alternative, held in reserve — serve the SPA for every friendly form.**
Achievable, and cheaper than it first looks, but only one implementation of it
is sane: a single catch-all route whose component asks the server where the path
points
(`fetch(path, { headers: { Accept: "application/json" }, redirect: "manual" })`
and read `Location`) and then renders the matching detail page. Enumerating a
route per edge — roughly seventeen, doubled by the stack depth — would put the
grammar in TypeScript as well as Go, which is the two-places-one-rule shape
`CLAUDE.md` keeps recording as the cause of real drift.

So: the shallow forms now, the catch-all in reserve. Asking the server where a
path points means adopting it later costs one component and one request, with no
grammar duplicated. There is no SEO consideration either way — Cetacean is not
indexed.

## Delivery

The three slices are separable, and only the first two are committed work. The
split is deliberate: the stack-scoped form and the traversal forms have very
different value-to-cost ratios, and shipping them together would have hidden
that.

### Phase 1 — accept a name in place of an ID (shipped)

`internal/api/canonical.go`, already in the tree: the single-pair form, the
`API014` ambiguity report, the read-grant gate, and the HTML exemption.

Nothing in the dashboard had to change for a name-addressed URL to render. A
detail route's parameter is handed straight to `api.service(param)`, and `fetch`
follows the `307` transparently, so `/services/shop_web` already fetches the
right resource.

It does not yet render *correctly*, though, and the two items this phase left
open — whether an SSE subscription survives the redirect, and where the
canonical ID has to be substituted — turn out to be one question with one
answer, given under 2a: the route parameter is resolved to the canonical ID
exactly once, and everything downstream of the first fetch uses the ID. Until
that lands, a name-addressed detail page is a page whose activity feed is
silently empty. Phase 1 is therefore complete as an *API* capability and
incomplete as a dashboard one, which is the honest way to describe it.

### Phase 2a — stack-scoped names

The `stack → services` edge, the compose-name matching and its exact-match
precedence, and the `/stacks/:stack/services/:name` route in the dashboard.

This is where the value is. `/stacks/shop/services/web` is the name in the file
the reader wrote, which is what makes it the form people type and paste, and it
is also what makes a breadcrumb mean something. It needs one edge — no adjacency
table, no reverse direction, no cross-reference scans.

Link generation switches to names for the types whose names are stable and
unique: services, configs, secrets, networks, volumes, stacks. **Nodes and tasks
keep ID URLs.** A node hostname can both collide — that is what `API014` is for
— and change, and a task has no name of its own, so keeping them on IDs means
the dashboard never has to render a disambiguation page, which is the only
expensive UI this design could otherwise force.

Docker's name charset (`[a-zA-Z0-9][a-zA-Z0-9_.-]*`) is URL-safe, so there is no
encoding work.

One accepted consequence: a name URL bookmarked today can point at a different
resource tomorrow, if a service is removed and recreated under the same name.
That is what addressing by name means, and it is why the canonical form stays
the ID.

#### Server

Build the **pair-chain parser** in 2a, with an edge table holding one row —
`stack → services`. 2b then adds rows rather than restructuring, which is what
makes "2b is cheap to pick up" true rather than aspirational. The single-pair
fast path must stay a map lookup: a path of two or three segments never
consults the edge table.

#### One link builder

`lib/searchConstants.ts`'s `resourcePath(type, id, name?)` already takes both an
ID and a name, and already returns name paths for volumes and stacks. It has
three consumers — `ActivityFeed`, `SearchPalette`, `SearchPage` — so teaching it
to prefer the name fixes those at once.

The other ~92 literal `` `/services/${id}` ``-shaped sites across `pages/` and
`hooks/` should migrate to it. That is the bulk of 2a's frontend diff; it is
mechanical, and it is worth doing on its own terms, because a single link
builder is how the stack-scoped form ends up consistent rather than appearing on
three pages out of twelve.

`resourcePath` needs the stack name to build the scoped form and does not
currently receive it. Search results and history entries carry a resource's name
but not its stack label, so either the signature grows a `stack?` parameter that
callers holding one supply, or the scoped form is reserved for pages that
already know the stack. The former is preferable; the latter is the fallback
where a caller cannot obtain it cheaply.

List pages need nothing: `useSwarmQuery` subscribes to the collection path
(`/services`), which carries no identifier and is never redirected.

#### Breadcrumbs already model this

`lib/resourceBreadcrumbs.ts` presents a stacked resource as
`Stacks › shop › web`, computing both the stack and the stripped leaf name
through `stripStackPrefix`, and `resourceParentPath` already sends the
post-removal redirect to `/stacks/<stack>`. The information architecture is
already stack-first — only the URL is not. The leaf's `to` becomes the
stack-scoped form and the trail needs no other change.

#### The route param resolves to the canonical ID exactly once

This is the rule that closes both of Phase 1's unverified items, and there are
three independent reasons for it rather than the one I first wrote down:

- The per-resource SSE subscription would otherwise depend on `EventSource`
  following a `307`. That is specified behaviour, but it is untested here and
  needing it at all is avoidable.
- `api.history({ resourceId })` is keyed by resource **ID**. A name returns an
  empty list, so a name-addressed detail page would silently show no activity —
  and `useDetailResource` passes its `key` straight through to that call today.
- `useDetailResource`'s React Query key *is* the `ssePath` string, so a name URL
  and an ID URL for one resource would occupy two cache entries and never share
  a fetch.

Concretely: `useDetailResource` keeps the route parameter as its query key — it
is stable and unique per URL — and takes the canonical ID from the **fetched
resource** for the SSE subscription and the history query, both of which then
enable only once the first fetch resolves. A detail page has nothing to render
before then in any case.

#### Docs

`docs/api.md`'s "Resource identifiers" section gains the stack-scoped form and
the compose-name rule.

#### What 2a does not need

The adjacency table beyond its one row, `API015`, relationship verification in
the reverse direction, and the O(services) cross-reference scans.

### Phase 2b — relationship traversals (deferred)

The full adjacency table, both directions, the two-level stack depth, per-pair
authorization and the shadowing invariant.

Deferred, not cancelled, and the reason is worth recording. The reverse forms
read backwards, nobody types them, and their only real use is making generated
links self-describing — which the dashboard does not need, because it links by
ID and already ACL-filters those lists. Against that: seventeen edges to
declare and keep in step with their resolvers, relationship verification,
authorization on every pair, and the O(services) cost of the reverse scans. The
grammar is specified so this is cheap to pick up, and the trigger for picking it
up is a concrete consumer that wants to *address* a relationship — a link in
documentation, an MCP resource URI, an export format — rather than the symmetry
being tidy.

Two pieces of 2b are worth extracting and landing with 2a instead, because
neither depends on a traversal URL ever being served:

- **`cluster.ResolveTask`,** because MCP resolves `<service>.<slot>` to a dead
  record today (see "Addressing a task"). That is a shipped defect, independent
  of whether any traversal URL is ever served.
- **The shadowing invariant test.** It costs nothing now and it is the thing
  that will silently break: adding `GET /services/{id}/networks/{x}` in a year
  would lose to the grammar with no failing test, and today the router has
  exactly one four-segment route (`/-/licenses/texts/{id}`), under a prefix no
  collection claims.

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

Phase 1's tests are in `internal/api/canonical_test.go` already, including the
two the implementation got wrong first time: the ambiguity report is withheld
without a type-level read grant, and the redirect is visible to `requestLogger`
and the self-metrics. Both were written by breaking the fix and watching them
fail, which is the standard the rest of this should meet.

**Phase 2a, server:**

- The compose name and the Docker name resolve to the same service, and an
  exact match beats a prefix-stripped one on a stack holding both `web` and
  `shop_web`.
- A service whose stack label names a different stack is not reachable through
  this stack's path.
- A two- or three-segment path never consults the edge table, so the common
  request keeps costing one map lookup.
- The shadowing invariant, brought forward from 2b because it costs nothing and
  is the thing that breaks silently: walk the router's registered patterns and
  assert none of them would parse as a pair chain.

**Phase 2a, dashboard:**

- `resourcePath` returns the stack-scoped form when it knows the stack and the
  flat name form when it does not, for every type it handles — a table test, so
  a type added later has to decide.
- A name-addressed detail page subscribes to the SSE path of the resolved **ID**
  and requests history for the resolved **ID**. Both are assertions about what
  the page asks for rather than what it renders, because the failure they guard
  is silent: an activity feed that is empty rather than wrong.
- The same resource reached by name and by ID produces one React Query cache
  entry, not two.

**`cluster.ResolveTask`, extracted from 2b:**

- A slot holding a live task and one or more replaced records resolves to the
  live one. This fails against the current `internal/mcp` implementation, which
  is the point of writing it.
- A slot holding only terminal records resolves to nothing rather than to the
  most recent of them.
- A global service's task resolves by node, and an unplaced one only by ID.
- The MCP and REST paths resolve one identifier identically, driven through both
  transports rather than through the shared function alone — that is the
  disagreement this whole design exists to have ended, and a test on the shared
  function cannot observe it.

**Phase 2b,** if it happens. The adjacency table is the single source of truth,
so the tests walk it rather than restating it:

- Every declared edge resolves, from a fixture holding one resource of every
  type wired to every other. This is the `TestTypeGrantsAgreesWithCan` pattern
  `CLAUDE.md` records for exactly this hazard: a table and the resolvers that
  read it drifting apart silently.
- Every reverse edge declared as reverse resolves in that direction too.
- An edge that does not hold answers `404` `API015`, naming both resources.
- Every pair's ACL is enforced independently: a grant on the target but not the
  context must not redirect, and vice versa.
- A traversal whose target identifier is already canonical still redirects
  (`/services/web/volumes/shop-data`), since the shortcut belongs to the
  single-pair form alone.

Two e2e lanes through the real binary, since the middleware sits in the chain and
the unit tests exercise it through a test router: one stack-scoped service read
in 2a, and one stack-scoped traversal to a config if 2b lands, both asserting the
`Location` and the followed response.

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
