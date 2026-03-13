# Contributing to Cetacean

Thanks for your interest in contributing! This guide covers everything you need to get started.

## Development Setup

**Prerequisites**: Go 1.26+, Node.js 22+, Docker with Swarm mode

```bash
# Clone and install frontend dependencies
git clone https://github.com/radiergummi/cetacean.git
cd cetacean
cd frontend && npm install && cd ..

# Init a local single-node swarm (if you don't have one)
docker swarm init

# Run backend and frontend dev server side by side:
go run .                              # Terminal 1: Go backend on :9000
cd frontend && npm run dev            # Terminal 2: Vite dev server on :5173
```

Open `http://localhost:5173`. Vite proxies resource paths to the Go backend, so you get hot-reload with live data.

## Running Checks

```bash
make check       # Full suite: lint + format check + tests
make test        # Go tests only
make lint        # golangci-lint + oxlint
make fmt         # Auto-format Go + frontend code
```

All checks must pass before submitting a PR. The CI pipeline runs the same checks.

## Submitting Changes

1. Fork the repository and create a branch from `main`
2. Make your changes — keep diffs focused on one concern
3. Add or update tests for any changed behavior
4. Run `make check` and ensure everything passes
5. Commit with a descriptive message (see below)
6. Open a pull request against `main`

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add volume detail cross-references
fix: correct SSE reconnection on network timeout
refactor: extract shared pagination logic
docs: update configuration reference
test: add integration tests for search endpoint
```

## Code Style

- **Go**: `gofmt` + `golangci-lint`. Match existing patterns — stdlib `net/http`, `log/slog`, no frameworks.
- **Frontend**: `oxlint` + `oxfmt`. React 19 with TypeScript, Tailwind CSS v4, shadcn/ui components.
- Don't refactor code unrelated to your change.

## Architecture

See the [README](README.md#architecture) for an overview. Key points:

- All API endpoints are read-only GET requests
- State lives in an in-memory cache fed by Docker event stream
- No separate domain models — uses Docker Engine API types directly
- Frontend uses per-resource SSE for real-time updates

## Reporting Issues

Open an issue on GitHub. Include:

- What you expected vs. what happened
- Steps to reproduce
- Cetacean version (`/api` endpoint shows version info)
- Docker Swarm version (`docker version`)

## License

By contributing, you agree that your contributions will be licensed under the [GPLv3](LICENSE).
