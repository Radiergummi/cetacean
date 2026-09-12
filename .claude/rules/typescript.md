---
paths:
  - "frontend/**/*.ts"
  - "frontend/**/*.tsx"
  - "website/**/*.ts"
  - "website/**/*.astro"
---

# TypeScript

- **No abbreviations.** `formatNumber`, not `fmtNum`; `index`, not `idx`. Industry-standard
  acronyms (URL, API, SSE, HTML) are fine.
- Brace every `if` body. Blank lines around logical blocks — after `if`, after declarations
  before logic, between `case` arms, before a trailing `return`.
- Destructure in callbacks: `({ value }) => value`.
- JSX props on separate lines at 3+ props or long lines.
- camelCase module constants (`knownStates`), `as const` where it applies.
- Multi-line JSDoc (`/**\n *\n */`).
- `exactOptionalPropertyTypes` is on, so a new optional prop needs `?: T | undefined`. Treat `!`
  as a hidden bug. `noPropertyAccessFromIndexSignature` and CI `--checkers` were measured and
  rejected — don't re-propose them.

## Types mirroring Docker

Optionality in `api/types.ts` comes from the Go struct tags — read them in
`$(go env GOMODCACHE)/github.com/docker/docker@<version>/api/types/`. `omitempty` does not apply
to struct fields, so container objects are always on the wire while their scalar leaves vanish
when zero. Guessing here is what caused the recurring `undefined` crashes.
