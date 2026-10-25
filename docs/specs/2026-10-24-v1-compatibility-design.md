# What v1 promises — design

Status: accepted.

## Problem

Nothing in the repository says what v1 means. 0.16.0, 0.17.0 and 0.18.0 each carried several
**Breaking** entries, and each was right on its own terms: strict `Allow`, `?filter=` strictness,
plugin privileges, `If-Match` on writes without their own `GET`, volume mounts needing `read`,
`null` lists over MCP, the problem `@context`. Before 1.0 that is what a `0.x` minor is for.
After it, a breaking change needs a major version, and a caller needs to know which parts of
the surface that applies to.

## What semver covers from 1.0

A change is breaking when a caller that worked against 1.x stops working, or silently gets a
different answer, without changing anything on its side. The covered surfaces:

| Surface | Covered | Source of truth |
|---|---|---|
| REST routes | path, method, status codes, `Allow`, `rel="edit"` targets, media types served per route | `api/openapi.yaml`, `internal/contract` route inventory |
| REST bodies | field names, types and meaning; the `@type` of each document | `api/openapi.yaml` |
| JSON-LD | every term `/api/context.jsonld` defines, and what it maps to | `internal/api/context.go` |
| Errors | problem-detail codes (`API003`, `SWM012`, …), the status each answers with | `internal/api/errors.go` |
| Pagination | query parameters, `Link` relations, `Range: items` and `Content-Range` | `docs/api.md` |
| Filter and sort | the `?filter=` grammar and the field names per resource, `?sort=` fields | `internal/filter`, `docs/api.md` |
| Feeds and CSV | which endpoints serve them; CSV column names and order | `docs/api.md` |
| SSE | event names, payload shapes, `Last-Event-ID` and `sync`/`full_sync` semantics | `/api/asyncapi` |
| MCP | tool names, argument names and types, resource URI templates, prompt names, error codes | the live server, via the `internal/contract` MCP inventory |
| Configuration | every setting's TOML path, env var, flag and default; `_FILE` variants | `docs/configuration.mdx` |
| ACL policy | grant syntax, resource types, permissions, audience prefixes | `docs/authorization.md` |
| Operations tiers | which tier each write needs | the tier table in `docs/configuration.mdx` |
| Metrics | `cetacean_*` metric names and label names | `/-/metrics` |
| On-disk state | `snapshot.json`, `oauth-tokens.json` and the DCR store stay readable by the next 1.x | `storage.data_dir` |
| Binary | the `healthcheck` subcommand and its exit codes | `main.go` |

Not covered, and stated as such in the docs:

- **Additions.** A new field, route, tool, argument, event, setting, error code or metric is a
  minor release. Clients must ignore what they don't know. This is already the rule for JSON
  bodies; it becomes the rule everywhere.
- **Wording.** Problem `title`/`detail`, tool descriptions, prompt text, log messages, the
  dashboard's text.
- **The HTML dashboard and its URLs beyond the resource routes.** It's the server's own client
  and ships with it.
- **Order** of list items under no `?sort=`, of JSON members, of `Allow` methods.
- **Defaults that track the outside world:** the recommended Docker Engine version, Go's TLS
  defaults, the MCP revisions offered.
- **Fixes that make behaviour match a cited specification.** CLAUDE.md already says a shortcut
  that passes our tests is still wrong. A fix that brings a header, status code or grammar in
  line with its RFC ships in a minor release with a **Breaking** changelog entry, as now. The
  bar is that the old behaviour contradicted a specification we cite, not that it was
  inconvenient.
- **Security fixes,** for the same reason. 0.18.0's "a volume mount needs `read`" is the model.

## Deprecation

1. A deprecated route, setting, tool, argument or field keeps working for **at least two minor
   releases** and is removed only in the next major.
2. **REST:** a deprecated route answers with `Deprecation` (RFC 9745) and, once a removal date is
   known, `Sunset` (RFC 8594), plus `Link: <…>; rel="deprecation"` to its changelog entry.
   OpenAPI marks it `deprecated: true`. A removed route answers `410` with `API012`, which
   0.13.0's topology removal already does.
3. **MCP:** a deprecated tool says so as the first sentence of its description and in
   `_meta["io.cetacean/deprecated"]` (`{"since": "1.3.0", "replacement": "find"}`), since the
   protocol has no deprecation annotation of its own. A deprecated argument is still accepted
   and documented as such.
4. **Configuration:** a deprecated setting is still read, logs one warning at startup naming
   its replacement, and is marked in `docs/configuration.mdx`. Setting both the old and the new
   one is a startup error, not a silent pick.
5. **Changelog:** a `### Deprecated` heading, as Keep a Changelog defines.

RFC 9745 and RFC 8594 go into `internal/spec/registry/http/` when the first deprecation ships,
and `unregistered.yaml`'s RFC 8594 entry goes then.

## Enforcement

The `internal/contract` inventories already list every route, MCP tool and resource type from
the router's source and the live server. A golden file per surface, committed and checked by
`make check`, turns an accidental removal into a failing test:

- routes with their methods and media types
- MCP tools with their argument names and types, resource templates, prompt names
- configuration keys from `knownenv_gen.go`
- problem-detail codes and metric names

An addition updates the golden file in the same commit. A removal or rename does too, and the
diff makes it visible in review. That's the whole mechanism: it doesn't decide whether a change
is allowed, it makes sure nobody makes one without noticing.

## Decided

1. **MCP output schemas.** Agents read tool output more loosely than API clients read JSON, and
   the digests are tuned for token cost. Tool and argument names, argument types, resource URIs
   and error codes are covered; tool result shapes are not, but change additively by default,
   which `docs/mcp-tools.mdx` is to say. A result field the widgets read is covered, because the
   widgets are clients that don't adapt.
2. **Widget bundles** (`ui://cetacean/*`). The URIs and the tool → widget mapping are covered;
   the bundles' contents aren't, since they ship with the server they render for.
3. **`application/vnd.cetacean.v1+json`** stays an alias of `application/json`. A 2.0 that
   changes bodies can serve `v1` beside `v2` for one major. No other version negotiation.
4. **Minimum Docker Engine API version.** `docs/getting-started.md` is to name it. Raising it is a
   minor release with a **Breaking** entry, the same as a cited-spec fix, because the engine's
   own support window decides it.
5. **Deprecation window.** Two minor releases, as set out under [Deprecation](#deprecation).

## Before 1.0

Each covered surface gets one sweep for what is still worth changing while it's cheap. Known
candidates, from the issues and the last three releases:

- **MCP:** conditional writes (#461) add an argument; adding it before 1.0 keeps the tools'
  shape settled. The spec registry should cover the revision we negotiate (#460).
- **REST:** sub-resource `GET`s other than labels and the service ones still send no `Allow`
  (noted in `2026-10-19-strict-allow-edit-links-design.md`).
- **Errors:** whether every code in `internal/api/errors.go` is still reachable.
- **Configuration:** settings whose names predate the TOML layout, if any read oddly beside
  their siblings.
- **Metrics:** label names, before dashboards are built on them.

Out of scope for 1.0, because each is additive and can land in a 1.x: the agent (#435),
resource creation, elicitation (#455) and its sub-issues. #456 might want to remove
`create_secret`'s inline value later; if that's likely, it should be deprecated before 1.0
rather than removed after.

The pre-1.0 work goes into a `v1.0` milestone.
