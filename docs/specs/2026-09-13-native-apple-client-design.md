# Native Apple Client Design

**Date:** 2026-09-13
**Status:** Investigation. Nothing here is committed to; the purpose is to establish whether a
native macOS/iOS client is worth building, what it would cost, and what the backend owes it.

## Summary

A native SwiftUI client for macOS and iOS, talking to one or more Cetacean deployments over the
existing HTTP API and SSE streams. No new backend transport: the API is already shaped for a
non-browser client, and the parts it is missing are small and independently useful.

The conclusion is that it works, that the API is in unusually good shape for it, and that the
honest cost of *full* dashboard parity on both platforms is five to seven months of focused
work — but that the valuable 20% (multi-cluster, menu bar, widgets, Spotlight, Shortcuts) is
six to eight weeks and does not require parity at all.

One backend change is a prerequisite rather than a nice-to-have: the REST API has no credential
a native app can obtain on its own. Cetacean already ships the machinery to fix that.

## Motivation

A wrapper around the existing SPA would be a week of work and worth nothing. The case for native
rests entirely on what the web dashboard *structurally cannot do*:

- **Multi-cluster.** A Cetacean process knows one swarm. Every deployment is its own origin, its
  own session, its own tab. Aggregating several clusters — one sidebar, one search, one health
  summary, one activity feed — cannot happen server-side without inventing a federation layer,
  and it falls out for free in a client that holds several connections at once. This is the
  single strongest argument for the app and the reason it is not a wrapper.
- **Presence without a window.** A menu bar item that is always showing cluster health, a widget
  on the desktop or Lock Screen, a complication. The dashboard requires a tab to be open and
  focused; most of the time what an operator wants is a glance.
- **The system as the surface.** Spotlight finding a service by name, Siri answering whether the
  cluster is healthy, a Shortcut scaling a service, a Live Activity tracking a rolling update to
  convergence. These are not skins over the API — they are places the API can be reached from
  that a browser has no access to.
- **Credentials and confirmation.** Keychain and the Secure Enclave for storage, Face ID or Touch
  ID in front of a tier-3 write, a Keychain identity for mTLS. Browser client-certificate UX is
  bad enough that `auth.mode = cert` is effectively an API-only mode today; natively it is the
  *best* mode.

## What the API already gives a native client

This is where the investigation got pleasantly surprising. The conventions that exist for protocol
correctness turn out to be exactly what a native client wants, and several of them matter more
off the browser than on it.

- **`Allow` on every GET** reports what the caller may actually do, after both `requireLevel` and
  `requireWriteACL`. It drives `.disabled()` on SwiftUI controls directly, with no client-side
  reading of the policy — the same rule `.claude/ARCHITECTURE.md` already states for the
  dashboard, and one the app gets for free rather than reimplementing.
- **ETag and `If-None-Match` on every JSON and Atom response.** `URLSession`'s shared cache honours
  these without any help, so a widget timeline reload or a background refresh costs a 304 and no
  parsing. A mobile client that refreshes opportunistically cares about this far more than a
  browser holding an open stream does.
- **SSE with `Last-Event-ID` replay** against the history ring. A phone backgrounds constantly; a
  client that can name the last event it saw and be told precisely what it missed — or be told the
  cursor is too old and to resync — is the difference between a live view and a stale one. The
  browser gets this as an optimisation. The app needs it.
- **RFC 9457 problem details plus the error registry at `/api/errors/{code}`.** Every failure has a
  stable code, a title, a suggestion and a documentation URL. That is a native error presentation
  with a "Learn more" button, from the wire, with nothing invented client-side.
- **Content negotiation, including the export formats.** `text/csv`, `application/atom+xml`,
  `text/vnd.graphviz`, `application/graphml+xml`, `application/vnd.jgf+json` all already exist.
  Drag-out, the share sheet, and Quick Look are a few lines each on top of representations the
  server emits today — the kind of integration that is expensive to build and trivial to expose.
- **JGF topology** means the graph structure arrives as a graph. The client supplies layout, not
  derivation.
- **`/history`** is the foundation for "what changed while you were away", which is what a widget,
  a notification and a launch-time digest all actually need.
- **Pagination `Link` headers** and `CollectionResponse.total` map onto `.onAppear`-driven paging
  the same way `DataTable`'s sentinel does.
- **JSON-LD `@id`** on every resource is a natural `NSUserActivity` / Core Spotlight identifier and
  a natural Handoff payload: a universal link, a Spotlight record and a deep link are the same
  string.

Scale, for sizing: 101 paths, 134 operations (81 GET, 22 PATCH, 14 POST, 8 PUT, 9 DELETE), 22 SSE
channels.

## What is missing

Three gaps. One blocks, one is a drift risk, one should be deferred.

### 1. There is no credential a native app can get (blocking)

`auth.Provider` coverage for a native client today:

| Mode | Native story |
|---|---|
| `none` | Works. Point at the URL. |
| `cert` | Works well. Keychain identity, `URLSession` challenge handler. Better than the browser. |
| `tailscale` | Works transparently if the device is on the tailnet, which the Tailscale app provides. |
| `oidc` | Partly. `OIDCProvider.Authenticate` accepts a Bearer **ID token** and validates `azp`. |
| `headers` | Cannot work. The credential is injected by a proxy the app is not behind. |

The `oidc` path is the problem. An ID token is short-lived, so the app needs a refresh flow, which
means being a registered public client in *each cluster's* IdP, with a custom redirect URI, added
by the operator, per deployment. That is friction nobody will accept for a third-party app.

Cetacean already has the answer and is not using it for this. `internal/mcp/oauth` is a full OAuth
2.1 authorization server: dynamic client registration (RFC 7591), PKCE S256 required, refresh
tokens, a consent screen, resource indicators (RFC 8707), CIMD, a published JWK Set. It mints
tokens for the MCP resource, and `auth.isExempt` routes `/mcp` around the provider middleware so
those tokens never reach the REST API.

**Recommendation:** extend that server to issue tokens for the API as a second resource, and teach
the auth middleware to accept them. The native flow then becomes: discover via
`/.well-known/oauth-authorization-server`, self-register via DCR, authorize in
`ASWebAuthenticationSession`, consent under whatever provider the deployment actually uses, hold a
refresh token in the Keychain. It works identically on all five providers, including `headers`
(the consent page runs behind the proxy; the app never sees the header), requires zero operator
configuration, and is revocable per device from the existing consent store.

Worth noting this is not native-only value: the same change gives any CLI, script or third-party
client a first-class credential, which the API does not have today.

Two smaller pieces fall out of it: the ACL `audience` model wants a token-derived audience so a
device grant can be narrowed below the user's own, and `/profile` is the natural capability probe
the app calls once per cluster to learn the operations tier and effective grants.

### 2. The domain types are not in the spec (drift risk)

`api/openapi.yaml` describes envelopes precisely and domain payloads as `type: object` —
`Spec: {type: object, description: Docker ServiceSpec}`. The Docker Engine API types *are* the
domain model, by design, and the spec does not restate them. The frontend consequently hand-mirrors
them: 73 types, 808 lines in `frontend/src/api/types.ts`.

So `swift-openapi-generator` would produce exactly the envelopes, and an opaque container at every
point that matters. Options:

1. **Hand-write Swift `Codable` mirrors** (~1,000 lines), with a test that decodes recorded
   fixtures from a live cluster so drift fails loudly. Cheapest, and the Docker API is versioned
   and slow-moving. Accepts a second hand-maintained mirror alongside the TypeScript one.
2. **Tighten the spec** with the real Docker schemas and generate both clients. Most work, pays off
   three times (web, native, and the MCP output schemas), removes the existing hand-mirror rather
   than adding to it.
3. **Generate from moby's own swagger** and bridge. Avoids writing the types but imports a large
   dependency shaped for a different API.

(1) to start, with (2) as the thing to do if a third client ever appears. Do not pick (1) *and*
quietly let it rot: the fixture test is the whole point.

### 3. Remote push needs a relay (defer)

A self-hosted Cetacean cannot send APNs pushes for an App Store app: the APNs key belongs to the
app's team, and distributing it to every deployment is not an option. This splits by platform:

- **macOS** is fine and does not need push at all. A menu bar app is a running process; it holds
  the SSE streams open and posts local notifications. Real-time alerting, no infrastructure.
- **iOS** gets foreground SSE, plus `BGAppRefreshTask` polling `/history` and `/recommendations`
  with `If-None-Match` and raising local notifications. Opportunistic, system-scheduled, roughly
  hourly at best — useful, but not alerting, and it must be described to users as such.

Real iOS push needs a relay the project operates, that clusters post to outbound with a per-device
token and that forwards to APNs. That is an operational commitment (uptime, abuse, the privacy
question of what transits it) well out of proportion to the rest of this work. Defer it. If it is
ever built, it should be opt-in, carry an event type and a resource `@id` and nothing else, and be
documented as a service the deployment chooses to talk to.

## Shape of the app

### Targets

One Swift package plus thin app targets, because widgets and intents run in separate processes and
need the same models, client and credential access:

```
CetaceanKit/                     (Swift package, multiplatform)
  Models/        Codable mirrors of the Docker + Cetacean types
  Client/        URLSession transport: negotiation, ETag, Link paging, problem details
  Streams/       SSE client with Last-Event-ID resume and batch framing
  Store/         one actor per cluster, Observable projections, registry
  Auth/          OAuth/DCR/PKCE, Keychain (access group), mTLS identity, Tailscale detection

Cetacean (macOS)        NavigationSplitView + Inspector, MenuBarExtra
Cetacean (iOS/iPadOS)   TabView / NavigationStack, same store
Widgets                 WidgetKit, shared App Group
Intents                 App Intents: Shortcuts, Siri, Spotlight actions, Controls
```

The cluster registry goes in an App Group container; credentials in a Keychain access group shared
by all four targets. Getting that wrong is the usual reason a widget cannot authenticate, so it is
a day-one decision rather than a refactor.

### State

One actor per cluster, owning: the last-seen history cursor, the decoded resource maps, the SSE
connection state, and the `Allow` sets. It reduces SSE events onto its maps the way the dashboard's
query cache does, and refetches on `sync`. Views observe projections. A cross-cluster aggregate
view observes all of them — which is the whole multi-cluster feature, and the reason the store is
per-cluster rather than global.

### Screen mapping

Most of this is mechanical, and the native idiom is usually *better* than what it replaces:

| Dashboard | Native |
|---|---|
| `DataTable` with sentinel paging | `Table` with sortable, user-reorderable, hideable columns, paging on appear |
| List/detail routing | `NavigationSplitView` (macOS) / `NavigationStack` (iOS), Inspector for the detail sidebar |
| `ResourceCard`, `InfoCard` | `GroupBox`, `LabeledContent`, `Grid` |
| Chart.js metrics | Swift Charts, with crosshair and selection built in |
| `ConnectionStatus` | Menu bar glyph, toolbar status item |
| Toasts (`sonner`) | Local notifications, or an inline banner |
| Service sub-resource editors | `Form` per panel, `If-Match` on submit, sheet on iOS, inspector on macOS |
| Search | `.searchable` plus `searchScopes` per resource type |
| Keyboard shortcuts (`ShortcutsHelp`) | `.keyboardShortcut`, menu bar commands — free, and discoverable |
| Topology (React Flow + elkjs) | The one genuinely hard screen; see below |

Topology is the outlier. The web view is `@xyflow/react` with `elkjs` layered layout, and there is
no Apple equivalent. Three ways out: port a layered layout onto SwiftUI `Canvas` (two to four
weeks, and the result is a graph view nobody asked for), render the JGF through Graphviz compiled
as a dependency, or keep a `WKWebView` over the existing SPA route for this screen alone (about
three days). The third is the right answer for v1 and possibly forever — hybrid is a failure only
when it is the default, and this is the one screen where the web implementation is genuinely better
than what a rewrite would produce. The same reasoning covers the Scalar API playground.

## Unique capabilities

Ordered by value over effort, which is roughly the order they should be built:

- **Multi-cluster everything.** Sidebar, search, activity, health roll-up across deployments. Not
  available anywhere else, at any price.
- **Menu bar extra.** Aggregate health, unhealthy services, open critical recommendations, and a
  jump list. The feature people will actually keep the app for.
- **Widgets.** macOS desktop, iOS Home and Lock Screen, StandBy, watch complication. Service
  health, replica counts, recommendation counts, last change. Cheap on top of
  `/cluster` + `/history` with ETags.
- **App Intents.** "Scale webapp to 5." "Is production healthy?" Siri, Spotlight actions, Shortcuts
  automations, Control Center controls, the Action button. Every intent is one existing endpoint,
  gated on the same `Allow` header, so the surface is additive rather than new.
- **Core Spotlight indexing** of services, stacks, nodes and secrets by name and label. System-wide
  search into a deep link, using `@id` as the record identifier.
- **Biometric gate on destructive writes.** Tier-3 operations (`DELETE`, node availability and
  role, CA rotation, unlock-key rotation) behind `LocalAuthentication`. Nothing the browser can do,
  and the operations tier already tells the app which requests deserve it.
- **Native mTLS.** A Keychain identity and a `URLSession` challenge handler turn `auth.mode = cert`
  from an API-only mode into the most pleasant one.
- **Live Activity for a rolling update.** Trigger an image change, watch replicas converge on the
  Lock Screen and in the Dynamic Island until `UpdateStatus.State` settles. The single best demo
  the project could have, and the data is already on the wire. (Note it needs push to update from
  outside the app, so it is a foreground-and-briefly-after feature until the relay question is
  settled.)
- **Drag-out and share** of the representations that already exist: topology as DOT or GraphML,
  any collection as CSV, any resource as JSON.
- **Handoff** between iPhone and Mac on the same resource, via `@id`.
- **Time-sensitive notifications** for node-down and critical recommendations, with a Focus filter
  so only production breaks through.

Deliberately out of scope: anything requiring exec or a terminal (Cetacean is read-mostly by
design), and any attempt to be a Compose editor.

## Effort

One experienced Swift developer, full-time. The parity target is the dashboard as it stands:
roughly 42,000 lines of TypeScript, of which `service-detail/` alone is 7,000 across twenty-odd
sub-resource editors, the log viewer 2,600 and metrics 3,100. That is the number to keep in mind
whenever "the same things the web interface does" comes up.

| Work | Estimate |
|---|---|
| Backend prerequisites (API-scoped OAuth tokens, token audience in the ACL) | 1–2 weeks (Go) |
| Foundation: models, client, SSE, ETag cache, Keychain, multi-cluster store | 3–4 weeks |
| Read-only parity: every resource type, list and detail, search, history, recommendations | 4–6 weeks |
| Log viewer: follow, filter, search, time range | 1–2 weeks |
| Metrics in Swift Charts | 1–2 weeks |
| Topology | 3 days (WebView) or 2–4 weeks (native) |
| Write surface: service sub-resources, node, config/secret, plugin, swarm ops, `If-Match` | 4–6 weeks |
| OS integration: menu bar, widgets, intents, Spotlight, biometrics, Handoff | 3–4 weeks |
| iOS adaptation | 2–3 weeks if shared from day one; far more if retrofitted |
| Accessibility, localisation seams, TestFlight, notarisation, CI | 2–3 weeks |

**Full parity, both platforms: five to seven months.** Evenings and weekends: multiply by three,
and expect the Docker type mirrors to have drifted by the time you finish.

**A read-only macOS app worth using: six to eight weeks**, if it is scoped as multi-cluster browse
plus menu bar plus widgets plus Spotlight, and topology is a web view. That version is a product.
Parity is not a prerequisite for it and chasing parity first is how it never ships.

## Phasing

- **Phase 0 — backend (1–2 weeks, Go).** API resource in the OAuth server; middleware accepts its
  tokens; `/profile` as the capability probe; pick the type-fidelity approach and write the
  fixture-drift test before any Swift exists.
- **Phase 1 — macOS, read-only (6–8 weeks).** Multi-cluster sidebar, all resource lists and
  details, search, history, recommendations, menu bar extra, widgets, Spotlight, Handoff. Topology
  as a web view. Ship it to TestFlight here, not later.
- **Phase 2 — the three hard read surfaces.** Logs, metrics, native topology if it has earned it.
- **Phase 3 — writes.** Service sub-resources first (it is where the tonnage is), biometric gating,
  App Intents over the same endpoints, optimistic concurrency surfaced as a real conflict UI rather
  than an alert.
- **Phase 4 — iOS.** Only once the shared package has been proven by two macOS phases. Live
  Activities, Controls, StandBy. Decide the push relay question on evidence about whether anyone
  wants alerting badly enough to accept a relay.

## Risks

- **Parity as the goal.** The dashboard is large and still growing; a native client chasing it
  is permanently behind and the OS integration — the only reason to build it — is always next
  sprint. The scope that works is "native where native wins".
- **Two hand-written type mirrors.** Mitigated by the fixture test, removed only by tightening the
  spec.
- **Auth fragmentation across five providers.** Phase 0 collapses it to one flow; without Phase 0
  it is five code paths and an unhappy `headers` mode.
- **App Store review of a tool that connects to arbitrary self-hosted servers.** Routine for this
  category, but worth a direct-distribution fallback (notarised, Sparkle) so a review cannot block
  a release. Mac App Store sandboxing is compatible with everything above; outgoing network and
  Keychain access groups are standard entitlements.
- **Platform version targets.** Widgets, Controls, Live Activities and the Spotlight action APIs
  have moved every year. Fix the minimum deployment targets against the then-current SDKs at Phase
  1 and check each capability above against its actual availability rather than against this
  document.

## Alternatives considered

- **Wrap the SPA in a WKWebView.** A week, and it delivers none of the motivation section. It does
  remain the right answer for individual screens (topology, the API playground).
- **Catalyst from the existing code.** There is no existing Apple code, and Catalyst is a path from
  iPad to Mac, not from React to either.
- **A CLI instead.** Cheaper and genuinely useful, but it competes with `docker` rather than
  complementing the dashboard, and it gets none of the glanceability. Notably it wants Phase 0 just
  as much, which is an argument for doing Phase 0 regardless of what follows it.
- **Lean on MCP instead of the REST API.** Tempting — the ACL path is already enforced below the
  transport and widgets already read this way. But MCP is shaped for agents: tool calls, not
  collections with pagination and validators, and no SSE resource streams in the form the app
  needs. The REST API is the better fit; MCP stays interesting as something the *Mac app* could
  expose over its aggregated multi-cluster view.
