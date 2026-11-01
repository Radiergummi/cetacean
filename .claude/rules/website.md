---
paths:
  - "website/**"
---

# Website

- A UI change to the site isn't done until `pnpm --filter website test` passes against a fresh
  `pnpm --filter website build`, WebKit included. CI runs only the Chromium half.
- Look at a changed page at 375px with touch emulation (the Playwright MCP) before calling it done;
  the sweep catches overflow and overlap, not ugliness.
- To debug a page, `pnpm --filter website audit-layout <path> <width> [--webkit]` runs the same
  checks against the dev server on :4321 and names the elements behind each finding.
- The sweep has no allowlist and gets none. A finding is fixed in the same change that
  surfaces it; a rule that misfires is corrected in `website/e2e/audit.ts`, not silenced per page.
