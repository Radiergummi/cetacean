# Strict Allow, and edit links for sub-resources — design

## Problem

A detail response's `Allow` lists the write methods of the resource *and of every sub-resource
under it*. `GET /secrets/{id}` offers `PATCH` because `/secrets/{id}/labels` accepts one;
`GET /services/{id}` offers `PUT` because `/scale` does. RFC 9110 §10.2.1 reads `Allow` as the
methods the target resource itself supports, so the header says something the specification
does not let it say.

The union also loses information the dashboard needs. One method name stands for several
sub-resources, and they need different operations levels:

| method on `/services/{id}` | sub-resources                                | levels  |
| -------------------------- | -------------------------------------------- | ------- |
| `PUT`                      | `scale`, `image` / `healthcheck`, `placement` / `endpoint-mode` | 1 / 2 / 3 |
| `PATCH`                    | `env`, `labels`, `resources`, … (all)        | 2       |
| `POST`                     | `restart`, `rollback`                        | 1       |

On `/swarm`, `PATCH` covers `dispatcher`, `orchestration` and `raft` at level 2 and `ca` and
`encryption` at level 3, so at level 2 the dashboard enabled the CA and encryption editors and
the server refused them. On services the same imprecision is papered over: the placement editor
is gated on `PATCH` although placement is a `PUT`, and the endpoint-mode editor sends its own
`HEAD` to learn what the union could not tell it.

## Shape

**`Allow` is strict.** Every response lists the methods of the path it answers, under the
caller's operations level and grants. A detail response's `DELETE` is still there when the
caller may delete; its sub-resources' writes are not.

**A detail response links each sub-resource the caller may write.** One `Link` value per
sub-resource, with the IANA-registered `edit` relation:

```http
Link: </services/abc/scale>; rel="edit", </services/abc/labels>; rel="edit"
```

A sub-resource is linked when any write route at that path is open to the caller: its level is
reached and the resource's write grant holds. The link says *that* the caller may write there,
not *how*; the client knows `scale` is a `PUT`. Only direct children are linked, which today is
every write sub-resource in the router.

Considered and rejected:

- **A body field** (`"writable": {"scale": ["PUT"]}`). It makes the representation per-caller,
  so the ETag and every SSE detail update would have to carry it too. Headers already vary by
  identity; the body does not.
- **Keeping the union and documenting it.** It was documented, and it is still the wrong answer
  to the question the dashboard asks, as the Swarm page shows.
- **`HEAD` on each sub-resource.** Twenty requests per service page, and most write-only
  sub-resources (`scale`, `restart`) have no `GET` to send a `HEAD` to.

**`Accept-Patch` follows `PATCH`.** RFC 5789 ties it to the resource that accepts the patch, so
it appears only on a response whose own `Allow` carries `PATCH`. Labels and service `env` accept
JSON Patch and merge patch; every other patchable path takes merge patch alone.

**A write method on a routed path answers `405`.** The SPA catch-all stood behind every path the
mux did not match by method, so `PUT /nodes` answered `404` with the dashboard. A method other
than `GET` or `HEAD` on a path some route serves now answers `405` with that path's `Allow`,
the way `/-/` paths already did.

## The dashboard

The client reads `rel="edit"` targets relative to the response's own URL, after redirects, and
keeps the names of the sub-resources: `scale`, `labels`. A page reached by a resource's name
still matches, because the links are built from the canonical path the redirect landed on.

Controls on the resource itself (delete) keep reading `Allow`. Controls on a sub-resource read
its link. The command palette and the recommendation fixes, which used to `HEAD` the parent and
check a method, check the parent's links instead.

## Not covered

Most sub-resource `GET` handlers set no `Allow` at all; three do. Giving every one its own
`Allow` and `Accept-Patch` is the same rule applied further, and is left for when a client needs
it: the dashboard reads the parent's links.
