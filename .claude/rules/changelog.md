---
paths:
  - "CHANGELOG.md"
---

# Changelog entries

One entry, one line. Twenty-five words is the target and forty is the ceiling; past that,
cut rather than wrap. New entries go under `[Unreleased]`.

Write what changed for someone *using* Cetacean, from their side of the screen. Name the
setting, endpoint or page they touch — that is what makes an entry actionable — and stop.

## Cut

- **The reasoning.** Why it was shaped that way belongs in the commit message or a design
  doc. An entry that explains itself is too long.
- **The mechanism.** Middleware, interfaces, caches, which function moved where. A user
  cannot see any of it.
- **The old behaviour**, beyond the half-clause it takes to say what is fixed. "X now does
  Y" already implies it did not.
- **Measurements and counts.** "twenty streams", "1046 KB to 326 KB", "three times less".
- **Entries a user would not notice at all**: refactoring, test coverage, internal renames,
  dependency bumps with no visible effect. Leave them out entirely.

## Consolidate

Three tweaks to one chart are one entry. If two entries share a subject, they are one entry.

## Shape

    - Volumes can be browsed and searched like every other resource.
    - `server.public_url` sets the canonical external URL once, instead of configuring the
      OAuth issuer and the OIDC redirect separately.
    - Fixed the dashboard going blank when a service was removed while its detail page was open.

Not:

    - The event streams now have a description of their own. `GET /api/asyncapi` serves an
      AsyncAPI 3.0 document covering all twenty streams — every resource list and detail URL,
      the global event stream, both log tails and the metrics stream — naming the events each
      one sends, what their payloads look like, and what happens when a client reconnects
      with a cursor. Sixteen of those streams were previously not described anywhere […]

An upgrade note is the one entry allowed a second sentence, and only to say what the operator
must do.
