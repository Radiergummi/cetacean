# Cache generation: cheap validators and memoised projections — design

## Problem

Every response is computed from nothing. The cache holds the whole cluster in memory and
only changes when Docker emits an event, but no reader can ask whether anything has
changed since it last looked, so each request re-derives an answer that is usually
identical to the last one.

Three measurements, taken at 1000 resources after the September allocation work:

- A conditional GET saves 12% of the CPU of a full response. `writeCachedJSONStatus`
  marshals the body and hashes it before it can compare `If-None-Match`, so a 304 does
  every expensive thing a 200 does and then declines to write the result.
- `GET /api/topology` costs 4.7ms, 7.7MiB and 24.5k allocations. An allocation profile puts
  85% of that in graph construction — `buildPlacementJGF` 48%, `buildNetworkJGF` 20%,
  `jgf.URN` 14% — and 13% in marshalling. Nothing about it depends on the request.
- A search copies every resource in the cluster. `Search` fans out eight goroutines and
  each calls a `ListX()`, so one query allocates a private copy of the whole cache.

These are one problem wearing three hats. A validator derived from the answer cannot be
cheaper than the answer, and a projection with no identity cannot be reused.

## What already exists, and the hole in it

`History.Append` increments a monotonic `uint64` on every mutation, and every `SetX` and
`DeleteX` on the cache routes through `notify` into it. SSE clients already depend on this
sequence: it is the `id:` they send back as `Last-Event-ID`. A generation counter is
therefore mostly already here.

The hole is that `notify` treats a sync event differently:

```go
if e.Type != EventSync {
    e.HistoryID = c.history.Append(...)   // increments
} else {
    e.HistoryID = c.history.Count()       // does not
}
```

`ReplaceAll` — the full resync that runs when the event stream is re-established — replaces
every resource map and then emits exactly one `EventSync`. So the entire contents of the
cache can change without the counter moving. Reused as a validator today, that would hand
a client a 304 for data that had been wholly replaced.

This is deliberate for history (a resync is not a list of changes a client can replay) and
wrong for a generation. The two need to stop being the same number.

## Scope

In: a generation counter distinct from the history sequence; a validator derived from it;
memoisation of the topology graph; a shared snapshot for search.

Out: memoising list responses. Lists are parameterised by search, filter, sort and
pagination, so the key space is unbounded and the hit rate is a guess. Lists get the cheap
validator — which is where their conditional-GET cost goes — and nothing more.

## The counter

`Cache.Generation() uint64`, backed by its own `atomic.Uint64`, incremented by `notify` for
every event including `EventSync`, and by `ReplaceAll` whether or not it emits one. The
history sequence keeps its current semantics untouched, because SSE replay depends on them.

Two numbers rather than one is the point: the history ID answers "which changes have I
missed", the generation answers "is anything I hold still true". A resync makes the second
question meaningful and the first unanswerable, which is exactly the case that breaks if
they share a counter.

The counter is read without the cache lock. A read that races a concurrent mutation may
observe the older value and produce a validator for state that has just been superseded —
the same window that exists today between marshalling a body and writing it, and it closes
on the next request.

## What a validator must include

This is the part that is easy to get wrong, and getting it wrong leaks one user's data to
another.

Today's validator is a hash of the rendered body, which makes it automatically correct: any
difference in what a caller sees produces a different tag. A generation-derived validator
has no such property. It is only as correct as the list of things we remember to mix in.

For a list or detail response, the body varies by:

- **the generation** — the cache contents;
- **the caller's effective grants** — `acl.Filter` runs on every list, and two identities
  see different rows for the same URL;
- **the ACL policy version** — `WatchPolicyFile` swaps the policy atomically at runtime, so
  what one identity may see changes with no cache mutation at all;
- **the query** — `search`, `filter`, `sort`, `dir`, `page`, `per_page`, and the range
  headers;
- **the negotiated media type** — the same resource is served as JSON, Atom, CSV or JSON
  Feed;
- **the base path**, which absolute `@id` values are built from.

The content coding is already handled: `codedETag` suffixes the tag, and that stays.

So the tag becomes a hash over a key struct rather than over the body:

```go
type validatorKey struct {
    generation  uint64
    policy      uint64   // acl policy version, bumped on SetPolicy
    grants      uint64   // fingerprint of this identity's effective grants
    query       string   // canonical, sorted
    mediaType   ContentType
    basePath    string
}
```

`grants` needs a small exported addition to `internal/acl`: `collectGrants` already computes
the effective grant list per identity, and a fingerprint over it — resources and permissions,
order-normalised — is a hash of a handful of short strings. It costs far less than
marshalling and hashing 350KiB, which is what it replaces.

The failure mode if a component is forgotten is silent and severe: a 304 telling a caller
their stale copy is current, or worse, a shared cache handing them somebody else's. The
testing section below is written around that rather than around the speedup.

A related gap this surfaces, pre-existing and worth closing at the same time: only
`atom_handlers.go` sets `Vary: Authorization, Cookie`. JSON list responses do not, even
though they are ACL-filtered. `Cache-Control: no-cache` forces revalidation so nothing is
served wrongly today, but the header is what tells an intermediary that these responses are
per-user, and it should be there on every ACL-filtered response.

## Topology

Topology is the projection worth memoising: expensive, unparameterised by query, and
recomputed identically for every request.

It is not, however, identical for every *caller* — `HandleTopology` ACL-filters services and
nodes before building either graph. So the memo is keyed by `(generation, grants)`, not by
generation alone. A deployment with no ACL policy has one key; a deployment with a handful
of roles has a handful.

A single-entry memo would thrash between two identities with different grants, so this wants
a small bounded map — on the order of sixteen entries, evicting the oldest generation
first. Beyond that it degrades to today's behaviour, which is the correct floor: a
deployment with hundreds of distinct grant sets gets no benefit and no regression.

What gets memoised is the marshalled JGF document, not the graph structure, so a hit skips
construction *and* marshalling *and* hashing — together all but a rounding error of the
allocations the profile attributes to this endpoint.

It applies too — with the key carrying the stack's name as well — to stack detail, which
turned out to be the second most expensive document the API builds. It does not apply to
`HandleCluster`, which at 128µs and 46 allocations is not worth a cache key, nor to anything
mixing in live data: `HandleStackSummary` queries Prometheus, and a memo keyed on the cache
generation would serve yesterday's numbers.

The GraphML and DOT renderings were in scope here and were not done. They start from the same
network graph and would key the same way, but they are not requested often enough to earn a
cache key — a browser renders the JGF document, and these two exist for exporting a graph by
hand. Left as they are deliberately.

## Search

Search is not memoisable — the query is the input — but its cost is not what it looks like.
The eight goroutines each copy a *different* resource type, so they are not duplicating one
another's work and there is no shared snapshot to consolidate: together they copy the
cluster exactly once. An allocation profile puts those copies at 62% of the bytes a query
allocates, spread evenly across the types.

The copy is therefore the cost, and the only way to remove it is not to take one. Search
reads a handful of fields per resource and keeps a small result; it never needs its own
copy of the cluster. Matching under the read lock and copying only the hits removes the
whole 62%.

Two constraints make that less mechanical than it sounds. `ListX()` returns sorted output
and search depends on it: results are appended in list order and truncated at `limit`, so
iterating a map directly would return different subsets and a different validator on every
call for the same query. The scan therefore yields in list order — gathering and sorting the
keys under the lock, which copies a string per resource rather than a struct. Sorting the
matches after collection was the plan and is worse: it reorders a page that the limit has
already truncated from the wrong end.

And the loop must not call back into the cache while holding the read lock. Go's `RWMutex`
is not re-entrant for readers: a writer arriving between two `RLock`s deadlocks the second.
The services branch calls `c.RunningTaskCount` inside its loop today, so enrichment has to
move after the lock is released. The other seven branches call nothing.

This is independent of the generation counter and lands first. It is in this document
because it is the third symptom of the same cause — a reader with no way to look without
taking a copy.

## Testing

The speedup is the easy part to verify and the least important. These tests are about the
validator being wrong:

- A full resync via `ReplaceAll` advances the generation, and a request holding the previous
  validator gets a 200 rather than a 304. This is the bug the current counter would have.
- Two identities with different grants never receive the same validator for the same URL,
  over lists, detail, topology and the feeds. Table-driven over the ACL fixtures already in
  `write_middleware_test.go`.
- A policy reload changes the validator for an identity whose grants changed, with no cache
  mutation in between.
- Every query parameter that changes a body changes the validator. Enumerated from the
  pagination and filter parsers rather than hand-listed, so a new parameter fails the test
  until it is added to the key.
- A 304 and a 200 for the same request agree on every header that is not the body.
- The memo returns a document byte-identical to an unmemoised build, across generations and
  grant sets.

On the benchmark side, `make bench` gains cases for a conditional GET that hits, and for
topology at a warm and a cold memo. The existing `BenchmarkHandleTopology` becomes the cold
case.

## Sequencing

The search snapshot is independent and lands first, on its own.

Then the counter, exposed and tested but unused, including the sync fix. Then the validator,
which is where the risk is — behind the tests above, and touching one writer
(`writeCachedJSONStatus`) before the others. Then the topology memo, which is pure upside
once the key is proven correct by the validator work.

Splitting it this way means the dangerous change ships alone, and the two cheap wins are not
held hostage to it.

## Files

- `internal/cache/cache.go` — `Generation()`, the counter, the `notify` sync fix.
- `internal/cache/history.go` — unchanged; called out because the temptation is to reuse
  `Count()`.
- `internal/acl/evaluator.go` — exported grant fingerprint, policy version.
- `internal/api/etag.go` — `validatorKey`, derivation, the writers.
- `internal/api/topology.go` — the memo.
- `internal/cluster/search.go` — matching under the read lock.
- `internal/cache/resourcemap.go` — `Each`, which is how it looks without copying.

## Rejected alternatives

**Hash the body, as today.** Correct by construction, and that is its whole appeal. But it
cannot be cheaper than rendering the body, which is the thing we are trying to avoid, so it
forecloses the entire point.

**Per-resource-type counters.** A node list would only invalidate when a node changed, which
is a better invalidation than one global counter gives. It also means every response that
reads more than one type — topology, cluster, stack detail, search — has to combine several,
and getting that combination wrong fails in the silent direction. One counter over-invalidates,
which is the safe way to be wrong. Revisit only if measurement shows over-invalidation
actually costs something.

**`Last-Modified` instead.** HTTP dates have one-second resolution. A cluster emits many
events per second during a rollout, which is exactly when a stale answer is most misleading.

**Memoising list responses too.** Unbounded key space, and the validator already removes the
cost that conditional GETs were supposed to remove. Revisit if profiling shows repeated
identical list queries in practice.

## Follow-ups, not in scope

`jgf.URN` is 14% of topology's allocations and `buildPlacementJGF` another 48%. Both become
per-generation rather than per-request costs once the memo lands, which is why neither is
worth optimising first — the memo subsumes them.

`ListServices` and `ListTasks` still sort 400-byte structs directly, and the index sort is
now the majority of `ResourceMap.List`. Removing the cache-level sort is the real win, but
it is load-bearing: `sortItems` returns items untouched when no `sort` is given, and
otherwise uses a stable sort that relies on the incoming order for ties. Removing it needs
`sortItems` to tie-break by ID and a contract change for the other callers of `List()`.

Two things measurement since has settled, both bigger than they look:

**A list copies the whole collection to return a page.** `ResourceMap.List` is 82% of the
bytes a node listing allocates — a thousand nodes copied out of the cache to send fifty.
Search fixed the equivalent by matching under the read lock, and a list cannot: `acl.Filter`
reaches `grantMatchesParts`, which calls the resolver, which is the cache. A scan holding
the read lock that filtered by ACL would deadlock on the first grant that needed a stack
resolved. Doing this means either projecting (name, sort key, id) under the lock and
fetching only the page afterwards, or giving the resolver a lock-free path. Both are
design changes, not tuning.

Two corrections to that paragraph, both from measuring it — see
`2026-09-14-acl-resolver-index-design.md`. The deadlock only bites if ACL filtering is fused
into the scan; projecting under the lock and filtering after it releases avoids both the copy
and the deadlock, and trades them for a torn read between the two passes. And the bytes were
the wrong target: the same list under a stack-scoped policy costs sixteen times what it costs
under a wildcard one while allocating identically, because the resolver scans every service to
resolve one name. That is being fixed first, after which this is worth re-measuring rather
than building.

**The derived validator stops at lists, and stays there.** Extending it to the detail
endpoints was tried and reverted: an ETag there is also the token `precond` compares an
`If-Match` against, and `precond` arrives at it by hashing the representation, so a GET that
tags itself from the generation refuses its own DELETE.
`TestPreconditionRoundTripsForEveryPairedEndpoint` catches it.

It is not worth going back for. The memo subsumed the motivation the way it subsumed
`jgf.URN`: a detail 304 is now a map lookup and a string compare, so deriving the validator
would save the lookup and cost `precond` a question about what a precondition means when the
GET and the write negotiated different media types. Detail endpoints keep a hashed validator,
which is correct by construction and, behind a memo, no longer expensive.
