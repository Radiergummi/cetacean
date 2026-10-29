---
paths:
  - "frontend/**/*.ts"
  - "frontend/**/*.tsx"
  - "website/**/*.ts"
  - "website/**/*.astro"
---

# TypeScript

- No abbreviations: `formatNumber`, not `fmtNum`; `index`, not `idx`. Common acronyms (URL, API) are fine.
- Brace every `if` body.
- `exactOptionalPropertyTypes` is on, so a new optional prop needs `?: T | undefined`. Treat `!` as a bug.
- `noPropertyAccessFromIndexSignature` and CI `--checkers` were measured and rejected. Don't propose them.
- Optionality in `api/types.ts` comes from the Go struct tags in
  `$(go env GOMODCACHE)/github.com/docker/docker@<version>/api/types/`. `omitempty` does not drop
  struct fields, so nested objects are always sent while zero scalars vanish. Don't guess:
  guessing caused the recurring `undefined` crashes.
