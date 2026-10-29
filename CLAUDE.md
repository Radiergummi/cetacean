# CLAUDE.md

`.claude/ARCHITECTURE.md` has the component map and the traps. Read it before you change a component.

## Invariants

- Docker Engine API types are the domain model (`swarm.Service`, `swarm.Node`). No separate structs.
- Volumes are keyed by Name (the route takes `{name}`). Everything else is keyed by ID.
- Secret data is cleared before every response: list, detail, stack, search, and MCP.
- Content negotiation never falls back to JSON. An endpoint refuses a type it cannot produce.
  Meta endpoints (`/-/health`, `/-/ready`) have no negotiation and no discovery links.
- Writes are gated twice: `requireLevel` (operations level) and `requireWriteACL` (per resource).
- Protocol work follows the RFC exactly (case rules, header syntax, status codes), not only our tests.
- A requirement from an external spec goes in `internal/spec/registry/`. The test that covers it
  claims it with `spec.Satisfies` as its first statement. The `mcp/sep-*.yaml` files are generated:
  edit `scripts/spec-genmcp` and run `make spec-genmcp`.
- `docs/configuration.mdx` is the only settings reference. Don't copy it. Name settings by TOML path.

## Comments

A comment says what the code cannot: an invariant, a trap, why a simpler version is wrong.
History, reasoning, and measurements go in the commit message. Don't name the test that pins it.

## Review

- Docs sometimes land before the code, on another branch. Before you call a setting or endpoint
  missing, check `gh pr list --state open` and say "not on `main`; see PR #N".
- The action versions in `.github/workflows/` are newer than your training data. They are correct.
- `new(expr)` is valid Go and `modernize` requires it. Copilot calls it a compile error; it is wrong.
