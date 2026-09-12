# Mobile / Responsive Layout Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Cetacean dashboard fully usable on mobile devices (390px+ viewport width) with near-full feature parity.

**Architecture:** Progressive enhancement — Tailwind breakpoint classes for CSS-level changes, a shared `useMatchesBreakpoint` hook for JS-level breakpoint checks. No new components, no mobile-specific routes. The `md:` breakpoint (768px) is the mobile/desktop boundary.

**Tech Stack:** React 19, Tailwind CSS v4, Chart.js, ReactFlow, Vitest

**Spec:** `docs/superpowers/specs/2026-03-16-mobile-responsive-design.md`

---

## Chunk 1: Foundation and List Pages

### Task 1: Create `useMatchesBreakpoint` hook

**Files:**
- Create: `frontend/src/hooks/useMatchesBreakpoint.ts`
- Create: `frontend/src/hooks/useMatchesBreakpoint.test.ts`

- [ ] **Step 1: Write the tests**

```typescript
// frontend/src/hooks/useMatchesBreakpoint.test.ts
import { renderHook, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { useMatchesBreakpoint } from "./useMatchesBreakpoint";

let listeners: Array<(e: { matches: boolean }) => void> = [];
let currentMatches = false;

beforeEach(() => {
  listeners = [];
  currentMatches = false;
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: currentMatches,
    media: query,
    addEventListener: (_: string, cb: (e: { matches: boolean }) => void) => {
      listeners.push(cb);
    },
    removeEventListener: (_: string, cb: (e: { matches: boolean }) => void) => {
      listeners = listeners.filter((l) => l !== cb);
    },
  }));
});

afterEach(() => vi.restoreAllMocks());

describe("useMatchesBreakpoint", () => {
  it('constructs max-width query for "below"', () => {
    renderHook(() => useMatchesBreakpoint("md", "below"));
    expect(matchMedia).toHaveBeenCalledWith("(max-width: 767px)");
  });

  it('constructs min-width query for "above"', () => {
    renderHook(() => useMatchesBreakpoint("md", "above"));
    expect(matchMedia).toHaveBeenCalledWith("(min-width: 768px)");
  });

  it("returns initial match state", () => {
    currentMatches = true;
    const { result } = renderHook(() => useMatchesBreakpoint("md", "below"));
    expect(result.current).toBe(true);
  });

  it("updates when media query changes", () => {
    const { result } = renderHook(() => useMatchesBreakpoint("md", "below"));
    expect(result.current).toBe(false);
    act(() => {
      for (const cb of listeners) cb({ matches: true });
    });
    expect(result.current).toBe(true);
  });

  it("cleans up listener on unmount", () => {
    const { unmount } = renderHook(() => useMatchesBreakpoint("md", "below"));
    expect(listeners).toHaveLength(1);
    unmount();
    expect(listeners).toHaveLength(0);
  });

  it("supports all Tailwind breakpoints", () => {
    renderHook(() => useMatchesBreakpoint("sm", "above"));
    expect(matchMedia).toHaveBeenCalledWith("(min-width: 640px)");

    renderHook(() => useMatchesBreakpoint("lg", "below"));
    expect(matchMedia).toHaveBeenCalledWith("(max-width: 1023px)");

    renderHook(() => useMatchesBreakpoint("xl", "above"));
    expect(matchMedia).toHaveBeenCalledWith("(min-width: 1280px)");

    renderHook(() => useMatchesBreakpoint("2xl", "below"));
    expect(matchMedia).toHaveBeenCalledWith("(max-width: 1535px)");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/hooks/useMatchesBreakpoint.test.ts`
Expected: FAIL — module not found

- [ ] **Step 3: Write the implementation**

```typescript
// frontend/src/hooks/useMatchesBreakpoint.ts
import { useEffect, useState } from "react";

const BREAKPOINTS: Record<string, number> = {
  sm: 640,
  md: 768,
  lg: 1024,
  xl: 1280,
  "2xl": 1536,
};

export function useMatchesBreakpoint(
  breakpoint: keyof typeof BREAKPOINTS,
  direction: "above" | "below",
): boolean {
  const px = BREAKPOINTS[breakpoint];
  const query =
    direction === "below"
      ? `(max-width: ${px - 1}px)`
      : `(min-width: ${px}px)`;

  const [matches, setMatches] = useState(() => matchMedia(query).matches);

  useEffect(() => {
    const mql = matchMedia(query);
    setMatches(mql.matches);
    const handler = (e: { matches: boolean }) => setMatches(e.matches);
    mql.addEventListener("change", handler);
    return () => mql.removeEventListener("change", handler);
  }, [query]);

  return matches;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/hooks/useMatchesBreakpoint.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/useMatchesBreakpoint.ts frontend/src/hooks/useMatchesBreakpoint.test.ts
git commit -m "feat: add useMatchesBreakpoint hook for responsive JS checks"
```

---

### Task 2: Make `useViewMode` mobile-aware

**Files:**
- Modify: `frontend/src/hooks/useViewMode.ts`
- Modify: `frontend/src/hooks/useViewMode.test.ts`

- [ ] **Step 1: Add tests for mobile behavior**

In `frontend/src/hooks/useViewMode.test.ts`, add the mock at the top level (after existing imports, before `beforeEach`). Vitest auto-hoists `vi.mock` calls. The mock defaults to `false` (desktop), which means all existing tests continue to behave as before. Then add a new `describe("mobile")` block inside the existing `describe("useViewMode")`:

```typescript
// Add after existing imports:
import { useMatchesBreakpoint } from "./useMatchesBreakpoint";

vi.mock("./useMatchesBreakpoint", () => ({
  useMatchesBreakpoint: vi.fn(() => false),
}));

const mockUseMatchesBreakpoint = vi.mocked(useMatchesBreakpoint);
```

```typescript
// Add as a nested describe inside the existing describe("useViewMode") block,
// after all existing tests:

describe("mobile", () => {
  beforeEach(() => mockUseMatchesBreakpoint.mockReturnValue(true));
  afterEach(() => mockUseMatchesBreakpoint.mockReturnValue(false));

  it("returns grid on mobile regardless of stored value", () => {
    store.set("viewMode:test", "table");
    const { result } = renderHook(() => useViewMode("test"));
    expect(result.current[0]).toBe("grid");
  });

  it("still persists user choice on mobile", () => {
    const { result } = renderHook(() => useViewMode("test"));
    act(() => result.current[1]("table"));
    expect(store.get("viewMode:test")).toBe("table");
    // But returns grid because mobile override
    expect(result.current[0]).toBe("grid");
  });

  it("returns stored value on desktop", () => {
    mockUseMatchesBreakpoint.mockReturnValue(false);
    store.set("viewMode:test", "table");
    const { result } = renderHook(() => useViewMode("test"));
    expect(result.current[0]).toBe("table");
  });
});
```

- [ ] **Step 2: Run tests to verify new tests fail**

Run: `cd frontend && npx vitest run src/hooks/useViewMode.test.ts`
Expected: FAIL — mobile tests fail (hook doesn't check breakpoint yet)

- [ ] **Step 3: Update implementation**

Replace `frontend/src/hooks/useViewMode.ts`:

```typescript
import { useCallback, useState } from "react";
import { useMatchesBreakpoint } from "./useMatchesBreakpoint";

export type ViewMode = "table" | "grid";

export function useViewMode(
  key: string,
  defaultMode: ViewMode = "table",
): [ViewMode, (m: ViewMode) => void] {
  const isMobile = useMatchesBreakpoint("md", "below");

  const [mode, setMode] = useState<ViewMode>(() => {
    const stored = localStorage.getItem(`viewMode:${key}`);
    return stored === "table" || stored === "grid" ? stored : defaultMode;
  });

  const set = useCallback(
    (m: ViewMode) => {
      setMode(m);
      localStorage.setItem(`viewMode:${key}`, m);
    },
    [key],
  );

  return [isMobile ? "grid" : mode, set];
}
```

- [ ] **Step 4: Run tests to verify all pass**

Run: `cd frontend && npx vitest run src/hooks/useViewMode.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/useViewMode.ts frontend/src/hooks/useViewMode.test.ts
git commit -m "feat: force grid view on mobile in useViewMode"
```

---

### Task 3: Hide ViewToggle on mobile

**Files:**
- Modify: `frontend/src/components/ListToolbar.tsx`

- [ ] **Step 1: Update ListToolbar to hide toggle on mobile**

In `frontend/src/components/ListToolbar.tsx` (lines 29-33), wrap the `<ViewToggle>` component in a div that hides it on mobile. Locate the conditional block that renders `ViewToggle` and wrap it:

```typescript
{viewMode != null && onViewModeChange && (
  <div className="hidden md:block">
    <ViewToggle
      mode={viewMode}
      onChange={onViewModeChange}
    />
  </div>
)}
```

Match the existing file's indentation (the file uses 2-space indentation).

- [ ] **Step 2: Verify visually**

Run: `cd frontend && npm run dev`
Open in browser at 390px width — toggle should be hidden. At 768px+ it should appear.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ListToolbar.tsx
git commit -m "feat: hide view toggle on mobile (cards only)"
```

---

### Task 4: Viewport meta tag

**Files:**
- Modify: `frontend/index.html`

- [ ] **Step 1: Update viewport meta**

In `frontend/index.html`, change line 10-13 from:

```html
<meta
  name="viewport"
  content="width=device-width, initial-scale=1.0"
/>
```

To:

```html
<meta
  name="viewport"
  content="width=device-width, initial-scale=1.0, viewport-fit=cover"
/>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add viewport-fit=cover for notched devices"
```

---

## Chunk 2: Charts, Metrics, and Topology

### Task 5: Disable brush-to-zoom on mobile

**Files:**
- Modify: `frontend/src/components/metrics/TimeSeriesChart.tsx`

- [ ] **Step 1: Add import and hook call**

Add the import at the file top of `frontend/src/components/metrics/TimeSeriesChart.tsx`:

```typescript
import { useMatchesBreakpoint } from "../../hooks/useMatchesBreakpoint";
```

Inside the component function, before the `useMemo` for options, add:

```typescript
const isMobile = useMatchesBreakpoint("md", "below");
```

- [ ] **Step 2: Conditionally disable drag zoom**

In the `useMemo` that builds `options` (around line 650), change the zoom plugin config from:

```typescript
drag: {
  enabled: true,
  backgroundColor: "rgba(100, 143, 255, 0.1)",
  borderColor: "rgba(100, 143, 255, 0.3)",
  borderWidth: 1,
  threshold: 5,
},
```

To:

```typescript
drag: {
  enabled: !isMobile,
  backgroundColor: "rgba(100, 143, 255, 0.1)",
  borderColor: "rgba(100, 143, 255, 0.3)",
  borderWidth: 1,
  threshold: 5,
},
```

And add `isMobile` to the `useMemo` dependency array (currently `[yMin, suggestedMax, stacked]`):

```typescript
[yMin, suggestedMax, stacked, isMobile],
```

- [ ] **Step 3: Verify visually**

Run: `cd frontend && npm run dev`
At 390px width, brush-to-zoom should not activate on drag. Range picker should still work.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/metrics/TimeSeriesChart.tsx
git commit -m "feat: disable brush-to-zoom on mobile, use range picker instead"
```

---

### Task 6: Fix NodeResourceGauges wrapping

**Files:**
- Modify: `frontend/src/components/metrics/NodeResourceGauges.tsx`

- [ ] **Step 1: Add flex-wrap to container**

In `frontend/src/components/metrics/NodeResourceGauges.tsx`, change line 69 from:

```typescript
<div className="flex items-center justify-center gap-8 py-2">
```

To:

```typescript
<div className="flex flex-wrap items-center justify-center gap-8 py-2">
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/metrics/NodeResourceGauges.tsx
git commit -m "feat: allow resource gauges to wrap on narrow viewports"
```

---

### Task 7: Adapt topology for mobile

**Files:**
- Modify: `frontend/src/pages/Topology.tsx`

- [ ] **Step 1: Import the breakpoint hook**

Add to imports in `frontend/src/pages/Topology.tsx`:

```typescript
import { useMatchesBreakpoint } from "../hooks/useMatchesBreakpoint";
```

- [ ] **Step 2: Use dynamic viewport height on mobile**

Inside both `LogicalView` and `PhysicalView` components, add at the top of each:

```typescript
const isMobile = useMatchesBreakpoint("md", "below");
```

In `LogicalView` (line 133-136), change the ReactFlow container from:

```typescript
<div
  className="relative"
  style={{ height: "calc(100vh - 12rem)" }}
>
```

To:

```typescript
<div
  className="relative"
  style={{ height: isMobile ? "calc(100dvh - 3rem)" : "calc(100vh - 12rem)" }}
>
```

In `PhysicalView` (line 168), change from:

```typescript
<div style={{ height: "calc(100vh - 12rem)" }}>
```

To:

```typescript
<div style={{ height: isMobile ? "calc(100dvh - 3rem)" : "calc(100vh - 12rem)" }}>
```

- [ ] **Step 3: Make StackLegend collapsible on mobile**

Replace the `StackLegend` function (lines 29-51) with:

```typescript
function StackLegend({ stackColors }: { stackColors: Map<string, string> }) {
  const isMobile = useMatchesBreakpoint("md", "below");
  const [open, setOpen] = useState(!isMobile);

  if (stackColors.size === 0) return null;

  if (isMobile && !open) {
    return (
      <button
        onClick={() => setOpen(true)}
        className="absolute bottom-3 right-3 z-10 rounded-lg border bg-card/90 p-2 shadow-sm backdrop-blur-sm"
        title="Show legend"
      >
        <Info className="size-4 text-muted-foreground" />
      </button>
    );
  }

  return (
    <div className="absolute bottom-3 left-3 z-10 rounded-lg border bg-card/90 p-3 text-xs shadow-sm backdrop-blur-sm">
      <div className="mb-1.5 flex items-center justify-between">
        <span className="font-medium text-muted-foreground">Stacks</span>
        {isMobile && (
          <button
            onClick={() => setOpen(false)}
            className="ml-2 text-muted-foreground hover:text-foreground"
          >
            <X className="size-3" />
          </button>
        )}
      </div>
      <div className="flex flex-col gap-1">
        {[...stackColors.entries()].map(([stack, color]) => (
          <span
            key={stack}
            className="flex items-center gap-1.5"
          >
            <span
              className="inline-block size-3 shrink-0 rounded-full"
              style={{ backgroundColor: color }}
            />
            {stack}
          </span>
        ))}
      </div>
    </div>
  );
}
```

Add `Info` and `X` to the lucide-react import on line 17 (currently `import { Network, Server } from "lucide-react"`). Change to:

```typescript
import { Info, Network, Server, X } from "lucide-react";
```

`useState` is already imported from React on line 18 — no change needed there.

- [ ] **Step 4: Verify visually**

Run: `cd frontend && npm run dev`
At 390px: topology should fill the screen, legend should show as a small info button. Clicking expands it, X closes it.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/Topology.tsx
git commit -m "feat: adapt topology for mobile (full-height, collapsible legend)"
```

---

## Chunk 3: Log Viewer, Search, and Header

### Task 8: Mobile-adapt log viewer

**Files:**
- Modify: `frontend/src/components/log/LogViewer.tsx`
- Modify: `frontend/src/components/log/LogTable.tsx`

- [ ] **Step 1: Default wrapLines on mobile and hide separators**

In `frontend/src/components/log/LogViewer.tsx`:

Add import:

```typescript
import { useMatchesBreakpoint } from "../../hooks/useMatchesBreakpoint";
```

Change line 39 from:

```typescript
const [wrapLines, setWrapLines] = useState(false);
```

To:

```typescript
const isMobile = useMatchesBreakpoint("md", "below");
const [wrapLines, setWrapLines] = useState(isMobile);
```

Change all three separator divs (lines 177, 188, 201) from:

```typescript
<div className="mx-0.5 h-5 w-px bg-border" />
```

To:

```typescript
<div className="mx-0.5 hidden h-5 w-px bg-border md:block" />
```

Change the search input (line 212) from:

```typescript
className="h-8 w-56 rounded-md border bg-background pr-16 pl-7 font-mono text-xs"
```

To:

```typescript
className="h-8 w-56 max-w-full rounded-md border bg-background pr-16 pl-7 font-mono text-xs"
```

- [ ] **Step 2: Hide task ID column on mobile in LogTable**

In `frontend/src/components/log/LogTable.tsx`:

Add import at the file top:

```typescript
import { useMatchesBreakpoint } from "../../hooks/useMatchesBreakpoint";
```

Call the hook once in the `LogTable` component function body (not in `LogRow` — it renders per row):

```typescript
const isMobile = useMatchesBreakpoint("md", "below");
```

Add `isMobile: boolean` to `LogRow`'s inline parameter type (around line 33-48, the destructured props object). Then pass `isMobile={isMobile}` from `LogTable` to `LogRow` at **both** render sites:
- The non-virtual path (around line 238)
- The virtual path (around line 327)

In `LogRow`, change the task ID column conditional (line 102) from:

```typescript
{showAttrs && (
```

To:

```typescript
{showAttrs && !isMobile && (
```

- [ ] **Step 3: Verify visually**

Run: `cd frontend && npm run dev`
At 390px: log lines should wrap by default, task ID column should be hidden, toolbar separators should be hidden, search input should not overflow.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/log/LogViewer.tsx frontend/src/components/log/LogTable.tsx
git commit -m "feat: adapt log viewer for mobile (wrap lines, hide task ID, fix overflow)"
```

---

### Task 9: Mobile-adapt SearchPalette

**Files:**
- Modify: `frontend/src/components/search/SearchPalette.tsx`

- [ ] **Step 1: Update modal container classes**

In `frontend/src/components/search/SearchPalette.tsx`, change line 213 from:

```typescript
className="mx-auto mt-[15vh] max-w-lg animate-[slide-down_150ms_ease-out] rounded-lg border bg-popover shadow-lg"
```

To:

```typescript
className="mx-4 mt-[5vh] max-w-lg animate-[slide-down_150ms_ease-out] rounded-lg border bg-popover shadow-lg md:mx-auto md:mt-[15vh]"
```

- [ ] **Step 2: Verify visually**

Run: `cd frontend && npm run dev`
At 390px: palette should have edge padding and sit higher on screen. At 768px+: should be centered as before.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/search/SearchPalette.tsx
git commit -m "feat: adjust search palette positioning for mobile"
```

---

### Task 10: Header touch targets and ConnectionStatus

**Files:**
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Hide ConnectionStatus on small screens**

In `frontend/src/App.tsx`, change line 79 from:

```typescript
<ConnectionStatus />
```

To:

```typescript
<span className="hidden sm:block">
  <ConnectionStatus />
</span>
```

- [ ] **Step 2: Bump hamburger button touch target**

Change line 90 from:

```typescript
className="rounded-md p-2 hover:bg-muted lg:hidden"
```

To:

```typescript
className="rounded-md p-2.5 hover:bg-muted lg:hidden"
```

- [ ] **Step 3: Bump nav link touch targets in mobile dropdown**

In the `NavLinks` component, change line 147 from:

```typescript
className="border-b-2 border-transparent px-3 py-2 text-sm text-muted-foreground transition-colors hover:text-foreground aria-[current=page]:border-foreground aria-[current=page]:font-medium aria-[current=page]:text-foreground"
```

To:

```typescript
className="border-b-2 border-transparent px-3 py-2.5 text-sm text-muted-foreground transition-colors hover:text-foreground aria-[current=page]:border-foreground aria-[current=page]:font-medium aria-[current=page]:text-foreground"
```

- [ ] **Step 4: Add safe area padding to main content**

In `frontend/src/App.tsx` line 113, the `<main>` element has `pb-48`. Add a safe area inset so content clears the iPhone home indicator. Change:

```typescript
<main className="mx-auto max-w-7xl px-4 py-6 pb-48 sm:px-6 lg:px-8">
```

To:

```typescript
<main className="mx-auto max-w-7xl px-4 py-6 pb-48 sm:px-6 lg:px-8" style={{ paddingBottom: "max(12rem, env(safe-area-inset-bottom))" }}>
```

This uses `max()` to keep the existing `pb-48` (12rem) on desktop while ensuring the safe area inset is respected on notched devices.

- [ ] **Step 5: Verify visually**

Run: `cd frontend && npm run dev`
At 390px: ConnectionStatus should be hidden. Hamburger and nav links should have larger tap targets. At 640px+: ConnectionStatus appears.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/App.tsx
git commit -m "feat: improve mobile header (touch targets, safe areas, hide status)"
```

---

## Chunk 4: Final Verification

### Task 11: Cross-page mobile verification

- [ ] **Step 1: Run all tests**

```bash
cd frontend && npx vitest run
```

Expected: All tests pass.

- [ ] **Step 2: Run lint and format checks**

```bash
cd frontend && npm run lint && npm run fmt:check
```

Expected: No errors.

- [ ] **Step 3: Type check**

```bash
cd frontend && npx tsc -b --noEmit
```

Expected: No type errors.

- [ ] **Step 4: Visual verification checklist**

Run `cd frontend && npm run dev` and test at 390px width:

- [ ] Cluster overview: health cards 2-column, metrics stack vertically
- [ ] Node list: card view forced, no toggle visible
- [ ] Service list: card view forced, no toggle visible
- [ ] Service detail: metadata stacks, metrics stack, tasks table scrolls
- [ ] Topology: full-height, legend is toggle button, pan/zoom works
- [ ] Log viewer: lines wrap, no task ID column, toolbar wraps cleanly
- [ ] Search palette (Cmd+K): edge padding, positioned near top
- [ ] Hamburger menu: opens, links are tappable, closes on navigate

- [ ] **Step 5: Commit any fixes from verification**

Stage only the specific files that were fixed, then commit:

```bash
git add <changed files>
git commit -m "fix: address issues found during mobile verification"
```

### Known Limitations (from spec)

- **TaskList**: No card view — keeps horizontal scroll on mobile. Follow-up: create `TaskCard` component.
- **Topology tap targets**: Node cards (`ServiceCardNode`, `TaskCardNode`, `PhysicalNodeCard`) may have sub-44px interactive elements. Audit during visual verification and fix if needed.
- **Touch target audit**: Only hamburger button and nav links are explicitly bumped. Theme toggle and other icon buttons should be checked during visual verification.
