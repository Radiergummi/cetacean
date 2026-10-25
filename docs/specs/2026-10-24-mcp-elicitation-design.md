# A human in the loop over MCP — design

Status: accepted, except the default of `mcp.inline_secret_values`. Covers #455 and the shape
of #456, #457 and #459; #458 is addressed under [Plugins](#plugins).

## Problem

Every MCP write rests on the arguments the model sends. A model that says it asked the user is
not evidence that it did, and some hosts show no approval prompt before a tool call. MCP
2026-07-28 replaces server-initiated `elicitation/create` with multi round-trip requests
(SEP-2322): a tool answers `resultType: "input_required"` with the questions it needs a person
to answer and an opaque `requestState`, and the client retries the call with `inputResponses`.
mcp-go 1.3.0 implements both sides (`server/mrtr.go`), including a bridge that issues the old
server-initiated requests to a client on an earlier revision.

## What elicitation is and isn't

**An elicited answer is as trustworthy as the client.** A conforming host shows the form to the
person and returns their answer; nothing stops a hostile client from returning `accept` itself.
So elicitation guards against a model acting on its own inside an honest host. It is not an
authorization mechanism, and it never replaces the operations tier or the ACL:

- Every check a write makes today — tier, `requireWriteACL`'s MCP counterpart, attachability,
  existence — runs **before** the tool elicits. A caller is never asked to confirm something
  they couldn't do, and the confirmation text can't reveal what they can't read.
- The same checks run **again** on the retried call, against the cache as it is then. A
  confirmation answers "may this happen", not "has it been authorized".

## The round trip

1. The first call runs its checks and builds the question. It answers `input_required` with
   one `elicitation/create` request under a fixed key per tool, and a `requestState`.
2. `requestState` is server-issued and **signed** (HMAC-SHA256 under a key generated at
   startup and kept in memory; a restart invalidates pending confirmations). It binds:
   - the caller's subject and provider
   - the tool name and a hash of its canonical arguments
   - the target's `Version.Index` where it has one
   - an expiry five minutes out
3. On the retried call, a state that fails to verify, has expired, or doesn't match the call
   (another caller, other arguments, a target that changed) is not an error: the tool starts
   over and asks again. The model can't confirm one thing and then swap the target.
4. An answer other than `accept` with the expected content makes no write. The tool result
   says the person declined or cancelled, as text the model can relay, with `isError: false`:
   declining is a normal outcome, not a failure.

The signing key lives with the MCP server, not the OAuth signer. The two have different
lifetimes, and a leaked confirmation state should not touch token signing.

## Capability gating

A 2026-07-28 client declares its capabilities per request
(`_meta["io.modelcontextprotocol/clientCapabilities"]`); an earlier one declares them at
`initialize`. A tool elicits only when the request's declared capabilities include
`elicitation` (and `elicitation.url` for URL mode). For an earlier client, mcp-go's bridge
needs a session that can carry server-initiated requests; where it has none, the client counts
as not supporting elicitation, rather than failing the call.

Without elicitation, each tool falls back as below. The fallback is per tool, because the
security cost differs.

## Per tool

### Impactful writes (#457)

The level-3 tools today are `update_node` (role and availability), `remove_service`,
`remove_task`, `remove_config`, `remove_secret`, `remove_network` and `remove_volume`. #457
also names `remove_stack`, `remove_node` and the plugin tools; MCP has none of them, and any
added later joins this list.

- **Question:** form mode, one boolean `confirm`, with a message built by the server. It names
  the resource and the effect, from what the cache holds, filtered by what the caller may read:
  - `remove_service`: the service and how many tasks it runs
  - `remove_secret`, `remove_config`, `remove_network`, `remove_volume`: which readable
    services use it, and how many unreadable ones do, without naming them
  - `update_node` drain: what `cluster.DrainImpactGraph` says moves
  - promote and demote: the manager count before and after, and whether quorum survives
  - what can't be undone
- **Shared with REST:** the impact text is computed in `internal/cluster`, as a structured
  value both the MCP message and the dashboard's confirmation dialogs can render. The dashboard
  computes its own today; moving it over is a follow-up, not a prerequisite.
- **Without elicitation:** behaves as today. A new setting `mcp.require_confirmation`
  (default `false`) refuses level-3 tools from such a client instead, with an error naming the
  setting. The default keeps every existing deployment working; an operator who wants the
  guarantee turns it on.
- **Tasks:** none of these tools is task-augmented, so the round trip always happens before any
  work. That becomes a rule: a tool that elicits does not declare task support. mcp-go
  doesn't implement the task `input_required` status yet, and a contract test holds the rule.

### Secret values (#456)

The spec forbids form-mode elicitation for sensitive data, so this is URL mode.

- `create_secret` called **without** `data`, from a client that supports URL elicitation,
  answers `input_required` with a URL to a new dashboard route,
  `/secrets/new?elicitation=<id>`. The page is prefilled with the name, labels and encoding
  the model chose. Those come from the server's pending record, not the query string.
- The person enters the value there, and it's sent with `POST /secrets` under their own
  session, with the usual tier and ACL checks, plus the elicitation ID.
- **Tying it back:** the server keeps the pending record (ID, subject, name, expiry) in
  memory. A `POST /secrets` carrying that ID completes it only when the browser session's
  subject matches the MCP caller's. Otherwise the page refuses, and says it was opened by
  someone else. The retried tool call succeeds once the record is complete, and returns the
  secret's ID, as today.
- **An unauthenticated caller can't use URL mode.** Under `auth.mode = none` or
  `mcp.auth_bypass` there is no subject to bind the browser to, so such a caller is treated as
  not supporting URL elicitation.
- **Inline values:**
  - From a client that supports URL elicitation, an inline `data` is refused. That client
    has a path that keeps the value out of the model, and accepting both would leave the
    model to pick the leaky one.
  - From a client without it, an inline value is accepted unless `mcp.inline_secret_values`
    is `false`.
  - **Decide** whether that setting defaults to `true` (no change for today's clients) or
    `false` (secure by default, so `create_secret` doesn't work from many hosts until they
    support URL mode). It belongs in #462's pre-1.0 sweep either way: flipping it after 1.0
    is breaking.

### Ambiguous hostnames (#459)

- **Question:** form mode, one string property with a titled `oneOf`, one entry per candidate
  node. Each entry's title carries:
  - the ID
  - the address
  - role and availability
  - the engine version
  - its labels
- **Who gets asked:** only callers who may read the hostname, as the ambiguity error is
  reported today (#454). Candidates the caller can't read are left out. If one readable
  candidate remains, it's the answer, and nobody is asked.
- **Where:** in the node resolver both tools and `resources/read` use. `resources/read` can
  answer `input_required` too (`InputRequestBuilder.ResourceResult`). Resources registered
  through `AddResource` return contents, not a result, so if that can't carry it, resources
  keep the error.
- **Without elicitation:** the error, as today.
- `requestState` binds the candidate set. If the set changed by the retry, the tool asks again.

### Plugins

There are no MCP plugin tools. #458 assumes install and upgrade tools that would take a
privilege list; REST has them, MCP doesn't. #458 stays open, re-scoped as the requirement on
any future plugin tool: its acceptance criterion becomes "no MCP plugin install or upgrade tool
ships without the review".

## Audit

The history doesn't record who made a change (#438), so it can't record that a person
confirmed one yet. Until #438 lands:
- Every completed confirmation logs at info, with the subject, provider, OAuth client, tool,
  target and outcome (`accepted`, `declined`, `cancelled`).
- A write made after a confirmation logs `confirmed: true` alongside its usual line.

#438's actor record gets a `confirmed` field from the start, and #457's history criterion
is met when it lands.

## Spec registry

A `sep-2322.yaml` family generated by `scripts/spec-genmcp`, claimed by the round-trip tests:
- the `input_required` shape
- state echo
- the retry carrying `inputResponses`
- the form-mode ban on sensitive data, which the URL-mode test for `create_secret` claims

URL-mode elicitation is in the base revision's elicitation page, so its requirements belong to
the regenerated base family from #460.

## Build order

1. The round-trip core: signed state, capability check, retry verification, one tool
   (`remove_service`) end to end, the SEP-2322 family. A contract test holds that no tool
   that elicits declares task support.
2. The other level-3 tools and `mcp.require_confirmation` (#457).
3. Hostname disambiguation (#459): small once the core exists.
4. URL mode and the dashboard route (#456). Last, because it is the only part that needs a
   new frontend route and a pending-record store.

## Decided

1. **`mcp.require_confirmation`** is a setting, defaulting to `false`, rather than an
   unconditional refusal of level-3 tools to clients without elicitation.
2. **#458** is re-scoped, not closed (see [Plugins](#plugins)).

## Decide

1. **`mcp.inline_secret_values`:** default `true` or `false` (see [Secret values](#secret-values-456)).
