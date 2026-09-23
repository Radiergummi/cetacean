# Contributing to Cetacean

Thanks for your interest in contributing. This guide covers everything you need to get started.

## Development setup

You need Go 1.26+, Node.js 24+ and Docker with Swarm mode.

```bash
git clone https://github.com/radiergummi/cetacean.git
cd cetacean

# Build once. The binary embeds frontend/dist and frontend/dist-widgets, so
# `go run .` does not compile until they exist.
make build

# Init a local single-node swarm (if you don't have one)
docker swarm init

# Run backend and frontend dev server side by side:
go run .                              # Terminal 1: Go backend on :9000
pnpm --filter frontend dev           # Terminal 2: Vite dev server on :5173
```

Open `http://localhost:5173`. Vite proxies resource paths to the Go backend, so you get hot-reload with live data.

## Running checks

```bash
make check       # Full suite: lint + format check + tests
make test        # Go tests only
make lint        # golangci-lint + actionlint + zizmor + oxlint + Vale
make fmt         # Auto-format Go + frontend code
make build       # Frontend, MCP widgets, and the binary that embeds them
```

All checks must pass before submitting a PR. The CI pipeline runs the same checks.

### End-to-end tests

`make test-stack` and `make e2e-up` run a local Docker Swarm against the real `./cetacean` binary. Neither
runs in CI—both need Docker and take minutes—so they aren't part of `make check`. See
[`test/e2e/README.md`](test/e2e/README.md) for prerequisites, running a single lane, and pointing the
Playwright suite (`make test-e2e`) at the environment.

## Submitting changes

1. Fork the repository and create a branch from `main`
2. Make your changes—keep diffs focused on one concern
3. Add or update tests for any changed behavior
4. Run `make check` and ensure everything passes
5. Commit with a descriptive message (see [Commit messages](#commit-messages))
6. Open a pull request against `main`

## Commit messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add volume detail cross-references
fix: correct SSE reconnection on network timeout
refactor: extract shared pagination logic
docs: update configuration reference
test: add integration tests for search endpoint
```

## Code style

- **Go**: `gofmt` + `golangci-lint`. Match existing patterns—stdlib `net/http`, `log/slog`, no frameworks.
- **Frontend**: `oxlint` + `oxfmt`. React 19 with TypeScript, Tailwind CSS v4, shadcn/ui components.
- **Workflows**: `actionlint` + `zizmor`. Pin every action by commit SHA with the version in a trailing comment.
- **Docs**: [Vale](https://vale.sh), configured in `.vale.ini`; see [Writing docs](#writing-docs).
- Don't refactor code unrelated to your change.

## Writing docs

`make lint-docs` enforces most of this. The rest is on you and the reviewer.

- American English, sentence-case headings, the Oxford comma, and em dashes without spaces (a—b).
- Contractions: *doesn't*, *isn't*, *can't*, and *it's* or *that's* where they read naturally. The full form
  stays where it ends a clause ("what it is.") or where the pronoun belongs to the phrase before it.
- Point within a page with *preceding*, *following*, *earlier*, or *later*, or a link. *Above* and *below*
  assume a visual layout; keep them for values ("above 90%") and the dashboard's own layout.
- Name settings by their TOML path (`server.trusted_proxies`), and link them to their entry in the configuration page.
- **swarm** is your cluster ("deploy to a swarm"); **Swarm** is Docker's orchestrator ("Swarm reschedules the task");
  the feature is **Swarm mode**.
- Verb or noun: *sign in*, *roll back*, and *set up* are verbs; *sign-in*, *rollback* and *setup* are nouns.
- **Compose file**, **config file**, **environment variable**, **command-line flag**, **health check**,
  **bearer token**, **operations level**, **refetch**.
- Name third-party tools the way their projects do: Alertmanager, cAdvisor, swarm-cronjob, Traefik.

To teach Vale a new term, add it to `.vale/styles/config/vocabularies/Cetacean/accept.txt`; that also fixes its
capitalization everywhere. A variant spelled differently goes in `.vale/styles/Cetacean/Terms.yml`.

## Architecture

See the [README](README.md#architecture) for an overview. Key points:

- All API endpoints are read-only GET requests
- State lives in an in-memory cache fed by Docker event stream
- No separate domain models—uses Docker Engine API types directly
- Frontend uses per-resource SSE for real-time updates

## Reporting issues

Open an issue on GitHub. Include:

- What you expected vs. what happened
- Steps to reproduce
- Cetacean version (`/api` endpoint shows version info)
- Docker Swarm version (`docker version`)

## License

By contributing, you agree to license your contributions under the [GPLv3](LICENSE).
