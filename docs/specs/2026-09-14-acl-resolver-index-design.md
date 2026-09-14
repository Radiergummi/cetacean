# The ACL resolver's stack lookup — design

## Problem

`Evaluator.grantMatchesParts` asks the resolver which stack a resource belongs to whenever a
grant does not cover that resource directly. The resolver is the cache, and `Cache.StackOf`
answers by scanning every resource of that type looking for one whose display name matches.

So an ACL-filtered list is O(items × grants × resources of that type). A thousand services
under a policy of twenty single-service grants is on the order of ten million string
comparisons, for one request.

## What measurement says

The same list endpoint at a thousand services, with and without the resolver that `main.go`
wires in production:

| policy                       | `resolver == nil`    | resolver wired       |
| ---------------------------- | -------------------- | -------------------- |
| `service:*`                  | 683µs / 626 allocs   | 705µs / 627 allocs   |
| twenty single-service grants | 1.66ms / 285 allocs  | 200ms / 290 allocs   |
| `stack:stack-0`              | 476µs / 56 allocs    | 11.2ms / 626 allocs  |

The middle row is the clean comparison and the one to read: the twenty grants match the same
twenty services either way, so both columns render the same response with the same
allocations, and the resolver costs 120 times the latency to arrive at an identical answer.
That is entirely CPU, and entirely `StackOf`.

The bottom row is not like-for-like and is included because it is the realistic policy.
Without a resolver a stack grant matches nothing, so that cell renders an empty list; with one
it renders the two hundred services in the stack. Some of the 11.2ms is therefore real output.
Not much of it: the top row renders five times as many services in 705µs.

A wildcard policy is unaffected because `grantCovers` matches before the resolver is
consulted. The cost appears exactly when a grant does *not* match directly, which is what a
stack-scoped or per-service policy is made of.

These figures come from a throwaway benchmark at twenty iterations. They are magnitudes, not
precise numbers; the committed benchmark described below is what pins those.

## Why this was invisible

`benchACL` in `internal/api/handlers_bench_test.go` builds an `Evaluator` with a policy and no
resolver. Every ACL figure the September allocation work recorded was therefore measured on a
path production does not take. The allocation claims hold — allocations are the same either
way — but the timings describe a configuration that does not ship.

`internal/acl`'s own tests do set a resolver, but a stub one whose `StackOf` is a map lookup
over a handful of fixtures. Nothing exercised the real one at size.

## Why the recorded follow-up aimed elsewhere

`2026-09-13-cache-generation-design.md` records that `ResourceMap.List` is 82% of the bytes a
node listing allocates, and proposes projecting under the lock so only a page is copied.

That is true about bytes and beside the point about time: a wildcard policy and a stack policy
allocate the same 520KiB and differ by sixteen times in latency. Projecting would not have
helped, either — ACL filtering runs over every item regardless of how few are fetched
afterwards, so the scan would have stayed exactly where it is while the endpoint looked
addressed.

The copy is still real. It is deferred rather than dismissed, below.

## Scope

In: a lookup index behind `StackOf`; wiring the resolver into the ACL benchmarks; the
`Vary: Authorization, Cookie` gap left open by the generation work.

Out: the list projection pipeline, which waits on a re-measurement once this lands.

## The index

`StackOf` keeps its signature, so `acl.ResourceResolver` and every caller are untouched. It
gains a `map[stackKey]string` keyed by `{type, display name}` and becomes a lookup.

Display name rather than ID, because that is what the evaluator holds: grants are written
against `service:webapp-api`, and the resolver is asked about names.

### It mirrors labels, not stack membership

This is the part that is easy to get wrong, and getting it wrong quietly changes who can see
what.

`StackOf` returns `labels["com.docker.stack.namespace"]` off the first resource whose name
matches. It never consults `c.stacks`. `addToStack` does consult it, and refuses to create a
stack entry unless a service is present — so a config labelled for a stack that has no
services resolves to that stack for ACL purposes while appearing in no stack listing.

An index built from `c.stacks` would therefore narrow every stack grant, silently, in the
direction that denies rather than the one that leaks. Safer than the opposite and still wrong.
The index records what the labels say.

### Where it is maintained

Not inside `addToStack` and `removeFromStack`, whose rules differ, but from the same call
sites: the `onSet` and `onDelete` hooks on the config, secret, network and volume resource
maps, which already run under the write lock. Services do not go through `ResourceMap`, so
`SetService` and `DeleteService` maintain it directly, and `ReplaceAll` rebuilds it with
everything else.

A rename has to move the entry rather than add a second one. `onSet` receives the previous
value, which is where the old key is removed.

Nodes and tasks have no case in `StackOf` today and get no entries. A lookup miss returns
`""`, which is what the absent `switch` arm returns now.

The locking rule is unchanged: `StackOf` still takes the read lock, so it still must not be
called from inside a scan that holds it.

## Making the cost visible

`benchACL` calls `SetResolver`, as `main.go` does.

This lands before the index, so `BenchmarkHandleListServices_ACL` reports the 200ms first.
A fix whose benchmark only ever showed the number after it is a fix nobody can review, and
leaving the helper as it is means the suite goes on measuring a configuration that does not
ship.

## Vary

`setAllow`, `setAllowList` and `setAllowSubResource` each add `Vary: Authorization, Cookie`.

These are the three functions that compute a per-identity `Allow`, so a response carrying one
is per-identity by construction and the two cannot drift apart. `listNotModified` already
calls one of them, so a 304 gets the header on the same path a 200 does.

`writeCachedAtom` keeps setting its own: feeds do not route through the allow seam.

`Cache-Control: no-cache` already forces revalidation, so nothing is served wrongly today.
The header is what tells an intermediary these responses are per-user, and it matters more now
than when the gap was recorded, because the derived validator made revalidation cheap enough
to actually happen.

## Testing

The speedup is the easy half. These are about the index disagreeing with the scan it replaces:

- Differential over a populated cache: the new `StackOf` answers what the old one answered,
  for every resource type it handles.
- A resource labelled for a stack with no services still resolves to that stack. This is the
  case that separates labels from membership, and the one an index built the obvious way gets
  wrong.
- A rename moves the entry rather than leaving the old name resolvable.
- A delete removes it.
- `ReplaceAll` rebuilds it, so a resync cannot leave a resolvable name behind for a resource
  that is gone.
- `Vary` appears once, carrying both tokens, on a list 200, a list 304 and a feed.

`internal/acl`'s existing tests use a stub resolver and are unaffected, which is the point of
leaving the interface alone.

## Sequencing

The benchmark wiring first, alone, so the cost is on the record before anything moves.

Then the index. Then `Vary`, which is independent of both and is only here because the
generation work left it open.

## Files

- `internal/cache/resolver.go` — `StackOf` becomes a lookup.
- `internal/cache/stacks.go` — the index, maintained beside stack membership and not from it.
- `internal/cache/cache.go` — `SetService`, `DeleteService`, `ReplaceAll`.
- `internal/api/handlers_bench_test.go` — `benchACL` wires the resolver.
- `internal/api/allow.go` — the three `setAllow*` functions.

## Rejected alternatives

**Memoise `StackOf` per request.** Resolving each item once instead of once per grant would
cut the twenty-grant case by twenty and leave the scan. It is a smaller change that trades a
quadratic for a linear that is still a scan, and it needs a per-request cache threaded through
the evaluator. The index costs one map and fixes the shape.

**Give the evaluator its own copy of the stack labels.** It would remove the resolver
interface and the re-entrancy rule with it. It also puts a second copy of cluster state
somewhere that has to be invalidated, which is the problem the cache exists to hold.

**Leave it and build the projection instead.** Measured above: it would not have helped.

## Follow-ups, not in scope

**The list still copies the whole collection to return a page.** `ResourceMap.List` is 82% of
the bytes a node listing allocates. Whether that is worth a two-pass pipeline is a question to
re-ask once the index lands and the time it was hiding behind is gone.

If it is taken up, two things are already settled. It belongs in `internal/cluster` rather
than `internal/api`, so the MCP transport reads the cluster the same way the REST one does.
And MCP needs a short-circuit to `List()` when no policy can deny anything: its reads return
whole collections, so projecting and then fetching every survivor is strictly worse than one
copy when there is nothing to filter out.

The deadlock the earlier document describes only bites if ACL filtering is fused into the
scan. Projecting under the lock and filtering after it releases avoids both the copy and the
deadlock, at the cost of a torn read between the two passes — which is the real question that
design has to answer.

**GraphML and DOT topology are not memoised**, though `2026-09-13-cache-generation-design.md`
put them in scope. They are not requested often enough to earn a cache key. Recorded there.
