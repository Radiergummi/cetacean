# Design specs

One file per feature, named `<date>-<feature>-design.md`. A design says what is
being built and why it is shaped that way; it stays here because the reasoning
outlives the work, and because the code it explains is the code someone reads
six months later without the conversation that produced it.

Implementation plans do not live here. They are working documents — task lists,
verification gates, running logs — and they stop being true the moment the work
lands, at which point a reader cannot tell a plan's "will" from the code's "is".
They belong in `docs/plans/`, which is gitignored, and both that directory and
`docs/specs/*-plan.md` are ignored so a plan written beside its design is not
committed by accident.

Point-in-time reviews and audits are ephemeral for the same reason and go to the
same place. What survives a review is the fix, the test that pins it, and — when
it changes how the code must be read — a note in `.claude/ARCHITECTURE.md`.

Two documents here are living rather than historical, and are maintained rather
than superseded:

- `2026-09-07-mcp-evaluation-set.md` — what people actually ask an agent about a
  Swarm cluster, with per-release coverage markers. Re-derive the markers each
  release rather than editing them in place, so the delta stays readable.
- `../test_protocol.md` — the same idea for the dashboard.
