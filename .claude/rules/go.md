---
paths:
  - "**/*.go"
---

# Go

- Standard `gofmt`; `golangci-lint run` must be clean (it includes `golines`).
- `new(expr)` is valid on Go 1.26 and `modernize` requires it — a `ptrTo` helper fails lint.
  Copilot review flags it as a compile error; that finding is wrong, so answer and resolve the
  thread rather than taking the edit.
- Docker Engine API types are the domain model (`swarm.Service`, `swarm.Node`). No separate structs.
