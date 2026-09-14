# CLAUDE.md

Cetacean is a read-mostly observability dashboard for Docker Swarm Mode. One Go binary with an
embedded React SPA: it reads the Docker socket, caches all swarm state in memory, and pushes
updates to browsers over SSE. The goal is to replace the Docker CLI for *understanding* a
cluster — every resource browsable, with working cross-references between them.

- `.claude/ARCHITECTURE.md` — the component map and the traps. Read it when changing a
  component, not before.
- `.claude/rules/` — per-language style, loaded when a matching file is opened.
- `docs/` — user documentation, published to the website.
- `docs/specs/*-design.md` — why a larger change is shaped the way it is.
- The rest of `.claude/` is machine-local and untracked: settings, worktrees, the backlog,
  the archived implementation plans.

## Commands

```bash
make check        # lint + typecheck + fmt-check + test — the gate
make build        # frontend + widgets + go build
make fmt          # gofmt + oxfmt (write)
make test         # go test ./...
make sbom         # regenerate the committed SBOM (rarely needed by hand)
```

The end-to-end suite is local-only, behind the `e2e` build tag, and drives the real binary
against a Docker-in-Docker swarm. `test/e2e/README.md` covers its lanes and known gaps.

```bash
make test-stack   # build instrumented + run the suite, reporting the coverage it reached
make e2e-up       # bring the environment up with a SUT on :19001, for the browser suite
make e2e-down     # tear it down
```

```bash
go build -o cetacean .          # needs frontend/dist and frontend/dist-widgets to exist
go test ./internal/cache/       # one package
go run .
```

```bash
cd frontend
npm run dev                     # Vite on :5173, proxies to :9000
npm run build                   # -> frontend/dist
npm run build:widgets           # -> frontend/dist-widgets (a go build prerequisite)
npm run check                   # tsc only, faster than a build
npx vitest run
```

```bash
cd website
npm run sync-assets             # generates src/data/errors.json; needed before `check` in a clean tree
npm run dev                     # Astro on :4321
npm run check                   # astro check, covers .astro as well as .ts
```

Docker: `docker build -t cetacean:latest .`, then `docker stack deploy -c compose.yaml cetacean`.
Add `-c compose.prometheus.yaml` to join the `monitoring` network from `compose.monitoring.yaml`.

## Configuration

66 settings, all optional, all with a TOML path, an env var and a flag.
**`docs/configuration.mdx` is canonical** — do not restate the table anywhere else, and
name settings by their TOML path (`server.trusted_proxies`) in user-facing text.

Secret settings also accept a `_FILE` suffix on the env var, which reads the value from a file
and ranks below the direct variable.

Locally you usually want `CETACEAN_PROMETHEUS_URL` (metrics are off without it) and
`CETACEAN_MCP=true` (MCP server at `/mcp`).

## Conventions

These bite across the codebase; the per-component rules live in `.claude/ARCHITECTURE.md`.

- Docker Engine API types are the domain model (`swarm.Service`, `swarm.Node`). No separate structs.
- **Volumes are keyed by Name; everything else by ID.** The volume route takes `{name}`.
- **Secret data is cleared before every response** — list, detail, stack, search, MCP alike.
- Stacks are derived from the `com.docker.stack.namespace` label, not a Docker primitive.
- Every response is content-negotiated: `Accept`, or a `.json`/`.html`/`.atom`/`.csv` suffix.
  An endpoint refuses a type it cannot produce; it does not fall back to JSON.
- JSON responses carry JSON-LD `@context`/`@id`/`@type`; lists return `CollectionResponse`
  with pagination `Link` headers; errors are RFC 9457 problem details.
- Every JSON and Atom response carries an ETag and honours `If-None-Match`.
- Writes are gated twice: `requireLevel` (operations tier 0–3) **and** `requireWriteACL`
  (per-resource). The `Allow` header on every GET reports what the caller may actually do.
- A rule that both the REST and MCP transports must apply belongs in `internal/cluster`.
  A projection only one of them renders does not — see the note at the end of that
  package's section in `.claude/ARCHITECTURE.md`.
- Protocol work follows the RFC exactly — case-insensitivity where the spec says so, header
  syntax, status codes. A shortcut that passes the tests we happened to write is still wrong.
- Structured logging via `log/slog` throughout.

## Code style

### Comments

- **A comment block caps at four lines.** If the explanation needs more room, it belongs in
  the commit message or a design doc under `docs/specs/` — not above the function.
- A comment states what the code cannot: an invariant, a constraint, a trap, a reason a
  tempting simpler version is wrong. The *argument for* a change — what it used to be, what
  it cost, why it beat the alternative — is commit-message material, every time.
- Don't name the test that pins the behaviour. Don't restate the identifier. Don't quote
  measurements. Don't re-explain a rule at each site that reads it; state it once where it
  lives and let the tests hold it.
- Match the terseness of the siblings: if `listFeeds` has a one-line doc, `searchFeeds` gets one.
- `.claude/ARCHITECTURE.md` is dense on purpose. It is not a model for source comments.

## Review

- Docs here sometimes land ahead of the code, on a separate branch. Before calling a documented
  setting or endpoint nonexistent, run `gh pr list --state open` and grep the open diffs; say
  "not on `main`; see PR #N". An unticked plan file is not evidence either.
- Don't flag the action versions in `.github/workflows/` as nonexistent. They are ahead of model
  training data and are correct as written.

## Releases

- **Sign release tags** — `git tag -s`, never `-a`. Unsigned tags show unverified, and
  immutable releases make it unfixable afterwards.
- **Update `CHANGELOG.md`** for every user-facing change — `.claude/rules/changelog.md`
  says how an entry is written.

## API documentation

- `api/openapi.yaml` — the web API, served at `GET /api` (Scalar playground for HTML).
- `internal/api/context.go` — the JSON-LD context, served at `GET /api/context.jsonld`.
- `docs/api.md` — the prose reference.

Meta endpoints (`/-/health`, `/-/ready`) have no negotiation and no discovery links.
