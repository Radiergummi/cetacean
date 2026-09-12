# Keyboard Affordances Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add comprehensive keyboard affordances — focus trapping, section navigation, resource prev/next, Alt-hold hints, table sort reachability, view mode shortcut, and Cmd/Ctrl+F override.

**Architecture:** Four new hooks (`useKeyboardHints`, `useSectionNavigation`, `useResourceNavigation`, `useSearchFocus`), two small components (`FocusTrap`, `useFocusRestore`), and targeted modifications to existing components. Focus trapping for modals uses Base UI Dialog's built-in `modal={true}`; inline editors use a lightweight sentinel-based FocusTrap. The `KeyboardHintProvider` context makes existing `ShortcutTooltip`s respond to Alt-hold.

**Tech Stack:** React 19, TypeScript, @base-ui/react@1.3.0, vitest + @testing-library/react

---

### Task 1: FocusTrap Component + useFocusRestore Hook

**Files:**
- Create: `frontend/src/components/FocusTrap.tsx`
- Create: `frontend/src/hooks/useFocusRestore.ts`
- Create: `frontend/src/components/FocusTrap.test.tsx`
- Create: `frontend/src/hooks/useFocusRestore.test.ts`

- [ ] **Step 1: Write FocusTrap test**

```tsx
// frontend/src/components/FocusTrap.test.tsx
import FocusTrap from "./FocusTrap";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect } from "vitest";

describe("FocusTrap", () => {
  it("wraps Tab focus within its children", async () => {
    const user = userEvent.setup();
    render(
      <FocusTrap>
        <button data-testid="first">First</button>
        <button data-testid="last">Last</button>
      </FocusTrap>,
    );
    screen.getByTestId("last").focus();
    await user.tab();
    expect(screen.getByTestId("first")).toHaveFocus();
  });

  it("wraps Shift+Tab focus within its children", async () => {
    const user = userEvent.setup();
    render(
      <FocusTrap>
        <button data-testid="first">First</button>
        <button data-testid="last">Last</button>
      </FocusTrap>,
    );
    screen.getByTestId("first").focus();
    await user.tab({ shift: true });
    expect(screen.getByTestId("last")).toHaveFocus();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/FocusTrap.test.tsx`
Expected: FAIL — module not found

- [ ] **Step 3: Write FocusTrap implementation**

```tsx
// frontend/src/components/FocusTrap.tsx
import type { ReactNode } from "react";
import { useRef } from "react";

/**
 * Lightweight focus trap using sentinel elements. Tab past the last focusable
 * element wraps to the first, and Shift+Tab past the first wraps to the last.
 * For portalled modals, prefer Base UI Dialog with modal={true} instead.
 */
export default function FocusTrap({ children }: { children: ReactNode }) {
  const containerRef = useRef<HTMLDivElement>(null);

  function getFocusable(): HTMLElement[] {
    if (!containerRef.current) {
      return [];
    }

    return Array.from(
      containerRef.current.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"]):not([aria-hidden="true"])',
      ),
    ).filter((element) => !element.dataset.focusSentinel);
  }

  function onSentinelFocus(edge: "start" | "end") {
    const focusable = getFocusable();

    if (focusable.length === 0) {
      return;
    }

    if (edge === "start") {
      focusable[focusable.length - 1].focus();
    } else {
      focusable[0].focus();
    }
  }

  return (
    <>
      <div
        tabIndex={0}
        aria-hidden="true"
        data-focus-sentinel
        onFocus={() => onSentinelFocus("start")}
        style={{ position: "fixed", opacity: 0, pointerEvents: "none" }}
      />
      <div ref={containerRef}>{children}</div>
      <div
        tabIndex={0}
        aria-hidden="true"
        data-focus-sentinel
        onFocus={() => onSentinelFocus("end")}
        style={{ position: "fixed", opacity: 0, pointerEvents: "none" }}
      />
    </>
  );
}
```

- [ ] **Step 4: Run FocusTrap test to verify it passes**

Run: `cd frontend && npx vitest run src/components/FocusTrap.test.tsx`
Expected: PASS

- [ ] **Step 5: Write useFocusRestore test**

```ts
// frontend/src/hooks/useFocusRestore.test.ts
import { useFocusRestore } from "./useFocusRestore";
import { renderHook } from "@testing-library/react";
import { describe, it, expect } from "vitest";

describe("useFocusRestore", () => {
  it("restores focus to previously active element on unmount", () => {
    const button = document.createElement("button");
    document.body.appendChild(button);
    button.focus();

    const { unmount } = renderHook(() => useFocusRestore());

    // Move focus elsewhere
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();

    unmount();
    expect(document.activeElement).toBe(button);

    document.body.removeChild(button);
    document.body.removeChild(input);
  });
});
```

- [ ] **Step 6: Write useFocusRestore implementation**

```ts
// frontend/src/hooks/useFocusRestore.ts
import { useEffect, useRef } from "react";

/**
 * Captures the active element on mount and restores focus to it on unmount.
 * No-op if the captured element is no longer in the DOM.
 */
export function useFocusRestore() {
  const previousRef = useRef<Element | null>(null);

  useEffect(() => {
    previousRef.current = document.activeElement;

    return () => {
      const element = previousRef.current;

      if (element instanceof HTMLElement && document.contains(element)) {
        element.focus();
      }
    };
  }, []);
}
```

- [ ] **Step 7: Run useFocusRestore test to verify it passes**

Run: `cd frontend && npx vitest run src/hooks/useFocusRestore.test.ts`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/FocusTrap.tsx frontend/src/components/FocusTrap.test.tsx \
  frontend/src/hooks/useFocusRestore.ts frontend/src/hooks/useFocusRestore.test.ts
git commit -m "feat(frontend): add FocusTrap component and useFocusRestore hook"
```

---

### Task 2: KeyboardHintContext + ShortcutTooltip Integration

**Files:**
- Create: `frontend/src/hooks/useKeyboardHints.ts`
- Create: `frontend/src/hooks/useKeyboardHints.test.ts`
- Modify: `frontend/src/components/ShortcutTooltip.tsx`
- Modify: `frontend/src/App.tsx:41-143` (Layout component)

- [ ] **Step 1: Write useKeyboardHints test**

```ts
// frontend/src/hooks/useKeyboardHints.test.ts
import { KeyboardHintProvider, useKeyboardHints } from "./useKeyboardHints";
import { render, screen, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

function HintDisplay() {
  const { hintsVisible } = useKeyboardHints();
  return <span data-testid="hints">{hintsVisible ? "visible" : "hidden"}</span>;
}

describe("useKeyboardHints", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows hints after holding Alt for 1 second", () => {
    render(
      <KeyboardHintProvider>
        <HintDisplay />
      </KeyboardHintProvider>,
    );

    expect(screen.getByTestId("hints")).toHaveTextContent("hidden");

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Alt" }));
    });

    act(() => {
      vi.advanceTimersByTime(1000);
    });

    expect(screen.getByTestId("hints")).toHaveTextContent("visible");
  });

  it("hides hints on Alt keyup", () => {
    render(
      <KeyboardHintProvider>
        <HintDisplay />
      </KeyboardHintProvider>,
    );

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Alt" }));
      vi.advanceTimersByTime(1000);
    });

    expect(screen.getByTestId("hints")).toHaveTextContent("visible");

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keyup", { key: "Alt" }));
    });

    expect(screen.getByTestId("hints")).toHaveTextContent("hidden");
  });

  it("does not show hints if Alt released before 1 second", () => {
    render(
      <KeyboardHintProvider>
        <HintDisplay />
      </KeyboardHintProvider>,
    );

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "Alt" }));
      vi.advanceTimersByTime(500);
      document.dispatchEvent(new KeyboardEvent("keyup", { key: "Alt" }));
      vi.advanceTimersByTime(600);
    });

    expect(screen.getByTestId("hints")).toHaveTextContent("hidden");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/hooks/useKeyboardHints.test.ts`
Expected: FAIL — module not found

- [ ] **Step 3: Write useKeyboardHints implementation**

```ts
// frontend/src/hooks/useKeyboardHints.ts
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";

interface KeyboardHintState {
  hintsVisible: boolean;
}

const KeyboardHintContext = createContext<KeyboardHintState>({ hintsVisible: false });

export function useKeyboardHints() {
  return useContext(KeyboardHintContext);
}

const HOLD_DELAY = 1000;

export function KeyboardHintProvider({ children }: { children: ReactNode }) {
  const [hintsVisible, setHintsVisible] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Alt") {
        return;
      }

      if (timerRef.current) {
        return;
      }

      timerRef.current = setTimeout(() => {
        setHintsVisible(true);
      }, HOLD_DELAY);
    }

    function onKeyUp(event: KeyboardEvent) {
      if (event.key !== "Alt") {
        return;
      }

      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }

      setHintsVisible(false);
    }

    function onBlur() {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }

      setHintsVisible(false);
    }

    document.addEventListener("keydown", onKeyDown);
    document.addEventListener("keyup", onKeyUp);
    window.addEventListener("blur", onBlur);

    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.removeEventListener("keyup", onKeyUp);
      window.removeEventListener("blur", onBlur);

      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, []);

  return (
    <KeyboardHintContext value={{ hintsVisible }}>
      {children}
    </KeyboardHintContext>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/hooks/useKeyboardHints.test.ts`
Expected: PASS

- [ ] **Step 5: Update ShortcutTooltip to read KeyboardHintContext**

Modify `frontend/src/components/ShortcutTooltip.tsx`:

The component currently shows on hover after 500ms. Add context integration: when `hintsVisible` is true from `useKeyboardHints()`, show immediately (skip hover timer). The hover behavior remains unchanged.

```tsx
// Replace the entire file content:
import { useKeyboardHints } from "../hooks/useKeyboardHints";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";

interface Props {
  keys: string[];
  children: ReactNode;
}

export default function ShortcutTooltip({ keys, children }: Props) {
  const [hovered, setHovered] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const { hintsVisible } = useKeyboardHints();

  const show = useCallback(() => {
    timerRef.current = setTimeout(() => setHovered(true), 500);
  }, []);

  const hide = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
    }

    timerRef.current = null;

    setHovered(false);
  }, []);

  useEffect(() => {
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, []);

  const visible = hovered || hintsVisible;

  return (
    <div
      className="relative"
      onMouseEnter={show}
      onMouseLeave={hide}
    >
      {children}
      {visible && (
        <div className="pointer-events-none absolute top-full left-1/2 z-50 mt-1.5 hidden -translate-x-1/2 lg:block">
          <div className="flex items-center gap-1.5 rounded-md border bg-popover px-2 py-1 text-[11px] whitespace-nowrap text-popover-foreground shadow-md">
            {keys.map((key, index) => (
              <span
                key={index}
                className="flex items-center gap-1"
              >
                {index > 0 && <span className="text-muted-foreground">then</span>}
                <kbd className="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded border bg-muted px-1 font-mono text-[10px] font-medium uppercase">
                  {key}
                </kbd>
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 6: Wrap Layout with KeyboardHintProvider in App.tsx + add ShortcutTooltip to ? button**

Modify `frontend/src/App.tsx`. In the `App` component (around line 224), wrap `<Layout>` with `<KeyboardHintProvider>`:

Add import at top:
```tsx
import { KeyboardHintProvider } from "./hooks/useKeyboardHints";
```

Wrap Layout (around line 230):
```tsx
<KeyboardHintProvider>
  <Layout>
    ...
  </Layout>
</KeyboardHintProvider>
```

The keyboard shortcuts button in Layout (around line 101-109) is already wrapped in `<ShortcutTooltip keys={["?"]}>`. Verify this is the case — if not, wrap it.

- [ ] **Step 7: Run all tests to verify nothing broke**

Run: `cd frontend && npx vitest run`
Expected: All tests pass

- [ ] **Step 8: Commit**

```bash
git add frontend/src/hooks/useKeyboardHints.ts frontend/src/hooks/useKeyboardHints.test.ts \
  frontend/src/components/ShortcutTooltip.tsx frontend/src/App.tsx
git commit -m "feat(frontend): add Alt-hold keyboard hint system"
```

---

### Task 3: DataTable Sortable Column Headers via Keyboard

**Files:**
- Modify: `frontend/src/components/DataTable.tsx:196-210` (thead)
- Modify: `frontend/src/components/DataTable.test.tsx`

- [ ] **Step 1: Write test for keyboard-triggerable column sort**

Add to `frontend/src/components/DataTable.test.tsx`:

```tsx
it("triggers onHeaderClick via Enter key on sortable column", () => {
  const onSort = vi.fn();
  const sortableColumns: Column<Item>[] = [
    { header: "ID", cell: ({ id }) => id, onHeaderClick: onSort },
    { header: "Name", cell: ({ name }) => name },
  ];
  render(
    <DataTable
      columns={sortableColumns}
      data={data}
      keyFn={({ id }) => id}
    />,
  );
  const header = screen.getByRole("button", { name: "ID" });
  fireEvent.keyDown(header, { key: "Enter" });
  expect(onSort).toHaveBeenCalledTimes(1);
});

it("makes sortable columns focusable", () => {
  const sortableColumns: Column<Item>[] = [
    { header: "ID", cell: ({ id }) => id, onHeaderClick: vi.fn() },
    { header: "Name", cell: ({ name }) => name },
  ];
  render(
    <DataTable
      columns={sortableColumns}
      data={data}
      keyFn={({ id }) => id}
    />,
  );
  const sortableHeader = screen.getByRole("button", { name: "ID" });
  expect(sortableHeader).toHaveAttribute("tabindex", "0");

  // Non-sortable header should not have role="button"
  const plainHeader = screen.getByText("Name");
  expect(plainHeader).not.toHaveAttribute("role", "button");
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/DataTable.test.tsx`
Expected: FAIL — no element with role "button" and name "ID"

- [ ] **Step 3: Update DataTable column headers**

Modify `frontend/src/components/DataTable.tsx`. In the `<thead>` section (around lines 197-210), update the `<th>` elements:

```tsx
{columns.map((column, index) => (
  <th
    key={index}
    tabIndex={column.onHeaderClick ? 0 : undefined}
    role={column.onHeaderClick ? "button" : undefined}
    data-clickable={column.onHeaderClick ? "" : undefined}
    className={`p-3 text-left text-sm font-medium ${
      column.className ?? ""
    } data-clickable:cursor-pointer data-clickable:select-none data-clickable:hover:bg-muted/80 outline-none focus-visible:ring-3 focus-visible:ring-ring/50`}
    onClick={column.onHeaderClick}
    onKeyDown={
      column.onHeaderClick
        ? (event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              column.onHeaderClick!();
            }
          }
        : undefined
    }
  >
    {column.header}
  </th>
))}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/components/DataTable.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/DataTable.tsx frontend/src/components/DataTable.test.tsx
git commit -m "feat(frontend): make sortable table columns keyboard-accessible"
```

---

### Task 4: useSectionNavigation Hook

**Files:**
- Create: `frontend/src/hooks/useSectionNavigation.ts`
- Create: `frontend/src/hooks/useSectionNavigation.test.tsx`

- [ ] **Step 1: Write test**

```tsx
// frontend/src/hooks/useSectionNavigation.test.tsx
import { useSectionNavigation } from "./useSectionNavigation";
import { renderHook } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// Mock useHotkeys to capture the registered handlers
const registeredHotkeys: Record<string, () => void> = {};
vi.mock("./useHotkeys", () => ({
  useHotkeys: (map: Record<string, () => void>) => {
    Object.assign(registeredHotkeys, map);
  },
}));

function setupSections() {
  const main = document.createElement("main");
  const buttons: HTMLButtonElement[] = [];

  for (let i = 1; i <= 3; i++) {
    const button = document.createElement("button");
    button.setAttribute("aria-expanded", i % 2 === 1 ? "true" : "false");
    button.setAttribute("data-testid", `section-${i}`);
    button.textContent = `Section ${i}`;
    main.appendChild(button);
    main.appendChild(document.createElement("div")); // content
    buttons.push(button);
  }

  document.body.appendChild(main);

  return { main, buttons };
}

describe("useSectionNavigation", () => {
  beforeEach(() => {
    for (const key of Object.keys(registeredHotkeys)) {
      delete registeredHotkeys[key];
    }
  });

  afterEach(() => {
    document.body.replaceChildren();
  });

  it("focuses the next section header on }", () => {
    const { buttons } = setupSections();
    renderHook(() => useSectionNavigation());

    buttons[0].focus();
    registeredHotkeys["}"]();

    expect(document.activeElement).toBe(buttons[1]);
  });

  it("wraps around to first section after last", () => {
    const { buttons } = setupSections();
    renderHook(() => useSectionNavigation());

    buttons[2].focus();
    registeredHotkeys["}"]();

    expect(document.activeElement).toBe(buttons[0]);
  });

  it("focuses the previous section header on {", () => {
    const { buttons } = setupSections();
    renderHook(() => useSectionNavigation());

    buttons[1].focus();
    registeredHotkeys["{"]();

    expect(document.activeElement).toBe(buttons[0]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/hooks/useSectionNavigation.test.tsx`
Expected: FAIL — module not found

- [ ] **Step 3: Write useSectionNavigation implementation**

```ts
// frontend/src/hooks/useSectionNavigation.ts
import { useHotkeys } from "./useHotkeys";
import { useCallback } from "react";

function getSectionHeaders(): HTMLElement[] {
  const main = document.querySelector("main");

  if (!main) {
    return [];
  }

  return Array.from(main.querySelectorAll<HTMLElement>("button[aria-expanded]"));
}

/**
 * Registers { and } hotkeys for jumping between CollapsibleSection headers
 * on detail pages. Wraps around at ends.
 */
export function useSectionNavigation() {
  useHotkeys({
    "}": useCallback(() => {
      const headers = getSectionHeaders();

      if (headers.length === 0) {
        return;
      }

      const currentIndex = headers.indexOf(document.activeElement as HTMLElement);
      const nextIndex = (currentIndex + 1) % headers.length;
      headers[nextIndex].focus();
    }, []),

    "{": useCallback(() => {
      const headers = getSectionHeaders();

      if (headers.length === 0) {
        return;
      }

      const currentIndex = headers.indexOf(document.activeElement as HTMLElement);
      const previousIndex = currentIndex <= 0 ? headers.length - 1 : currentIndex - 1;
      headers[previousIndex].focus();
    }, []),
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/hooks/useSectionNavigation.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/useSectionNavigation.ts frontend/src/hooks/useSectionNavigation.test.tsx
git commit -m "feat(frontend): add section navigation with { and } keys"
```

---

### Task 5: useSearchFocus Hook (Cmd/Ctrl+F Override)

**Files:**
- Create: `frontend/src/hooks/useSearchFocus.ts`
- Create: `frontend/src/hooks/useSearchFocus.test.tsx`

- [ ] **Step 1: Write test**

```tsx
// frontend/src/hooks/useSearchFocus.test.tsx
import { useSearchFocus } from "./useSearchFocus";
import { renderHook } from "@testing-library/react";
import { describe, it, expect, afterEach } from "vitest";

describe("useSearchFocus", () => {
  afterEach(() => {
    document.body.replaceChildren();
  });

  it("focuses input on first Cmd+F", () => {
    const input = document.createElement("input");
    document.body.appendChild(input);
    const ref = { current: input };

    renderHook(() => useSearchFocus(ref));

    const event = new KeyboardEvent("keydown", {
      key: "f",
      metaKey: true,
      cancelable: true,
    });
    const prevented = !document.dispatchEvent(event);

    expect(document.activeElement).toBe(input);
    expect(prevented).toBe(true);
  });

  it("does not prevent default when input already focused", () => {
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    const ref = { current: input };

    renderHook(() => useSearchFocus(ref));

    const event = new KeyboardEvent("keydown", {
      key: "f",
      metaKey: true,
      cancelable: true,
    });
    const prevented = !document.dispatchEvent(event);

    expect(prevented).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/hooks/useSearchFocus.test.tsx`
Expected: FAIL — module not found

- [ ] **Step 3: Write useSearchFocus implementation**

```ts
// frontend/src/hooks/useSearchFocus.ts
import type { RefObject } from "react";
import { useEffect } from "react";

/**
 * Intercepts Cmd/Ctrl+F to focus an in-page search input. If the input is
 * already focused, the event passes through to the browser's native find.
 */
export function useSearchFocus(inputRef: RefObject<HTMLElement | null>) {
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (!(event.metaKey || event.ctrlKey) || event.key !== "f") {
        return;
      }

      const input = inputRef.current;

      if (!input) {
        return;
      }

      if (document.activeElement === input) {
        return;
      }

      event.preventDefault();
      input.focus();
    }

    document.addEventListener("keydown", onKeyDown);

    return () => document.removeEventListener("keydown", onKeyDown);
  }, [inputRef]);
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/hooks/useSearchFocus.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/useSearchFocus.ts frontend/src/hooks/useSearchFocus.test.tsx
git commit -m "feat(frontend): add Cmd/Ctrl+F search focus override hook"
```

---

### Task 6: useResourceNavigation Hook

**Files:**
- Create: `frontend/src/hooks/useResourceNavigation.ts`
- Create: `frontend/src/hooks/useResourceNavigation.test.tsx`

- [ ] **Step 1: Write test**

```tsx
// frontend/src/hooks/useResourceNavigation.test.tsx
import { useResourceNavigation } from "./useResourceNavigation";
import { renderHook, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { ReactNode } from "react";

// Mock useHotkeys
const registeredHotkeys: Record<string, () => void> = {};
vi.mock("./useHotkeys", () => ({
  useHotkeys: (map: Record<string, () => void>) => {
    Object.assign(registeredHotkeys, map);
  },
}));

// Mock navigate
const mockNavigate = vi.fn();
vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useSearchParams: () => [new URLSearchParams(), vi.fn()],
  };
});

// Mock api
vi.mock("../api/client", () => ({
  api: {
    nodes: () => Promise.resolve({ items: [
      { ID: "aaa" },
      { ID: "bbb" },
      { ID: "ccc" },
    ], total: 3 }),
  },
}));

function wrapper({ children }: { children: ReactNode }) {
  return <MemoryRouter>{children}</MemoryRouter>;
}

describe("useResourceNavigation", () => {
  beforeEach(() => {
    mockNavigate.mockClear();

    for (const key of Object.keys(registeredHotkeys)) {
      delete registeredHotkeys[key];
    }
  });

  it("navigates to next resource on ]", async () => {
    renderHook(
      () =>
        useResourceNavigation({
          resourceType: "nodes",
          currentId: "bbb",
          basePath: "/nodes",
        }),
      { wrapper },
    );

    await waitFor(() => expect(registeredHotkeys["]"]).toBeDefined());

    registeredHotkeys["]"]();
    expect(mockNavigate).toHaveBeenCalledWith("/nodes/ccc");
  });

  it("wraps around from last to first on ]", async () => {
    renderHook(
      () =>
        useResourceNavigation({
          resourceType: "nodes",
          currentId: "ccc",
          basePath: "/nodes",
        }),
      { wrapper },
    );

    await waitFor(() => expect(registeredHotkeys["]"]).toBeDefined());

    registeredHotkeys["]"]();
    expect(mockNavigate).toHaveBeenCalledWith("/nodes/aaa");
  });

  it("navigates to previous resource on [", async () => {
    renderHook(
      () =>
        useResourceNavigation({
          resourceType: "nodes",
          currentId: "bbb",
          basePath: "/nodes",
        }),
      { wrapper },
    );

    await waitFor(() => expect(registeredHotkeys["["]]).toBeDefined());

    registeredHotkeys["["]();
    expect(mockNavigate).toHaveBeenCalledWith("/nodes/aaa");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/hooks/useResourceNavigation.test.tsx`
Expected: FAIL — module not found

- [ ] **Step 3: Write useResourceNavigation implementation**

```ts
// frontend/src/hooks/useResourceNavigation.ts
import { api, type ListParams } from "../api/client";
import { useHotkeys } from "./useHotkeys";
import { useCallback, useEffect, useRef } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

type ResourceType = "nodes" | "services" | "tasks" | "stacks" | "configs" | "secrets" | "networks" | "volumes";

interface Options {
  resourceType: ResourceType;
  currentId: string;
  basePath: string;
}

const listFetchers: Record<ResourceType, (params?: ListParams) => Promise<{ items: unknown[]; total: number }>> = {
  nodes: api.nodes,
  services: api.services,
  tasks: api.tasks,
  stacks: api.stacks,
  configs: api.configs,
  secrets: api.secrets,
  networks: api.networks,
  volumes: api.volumes,
};

function extractId(item: unknown, resourceType: ResourceType): string {
  const record = item as Record<string, unknown>;

  if (resourceType === "volumes" || resourceType === "stacks") {
    return String(record.Name ?? record.name ?? "");
  }

  return String(record.ID ?? "");
}

/**
 * Fetches the resource list and registers [ and ] hotkeys for navigating
 * between detail pages in list order. Wraps around at ends.
 */
export function useResourceNavigation({ resourceType, currentId, basePath }: Options) {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const listRef = useRef<string[]>([]);
  const currentIdRef = useRef(currentId);
  currentIdRef.current = currentId;

  useEffect(() => {
    const params: ListParams = {};
    const sort = searchParams.get("sort");
    const dir = searchParams.get("dir") as "asc" | "desc" | null;

    if (sort) {
      params.sort = sort;
    }

    if (dir) {
      params.dir = dir;
    }

    params.limit = 0;

    const fetcher = listFetchers[resourceType];

    if (!fetcher) {
      return;
    }

    fetcher(params).then((response) => {
      listRef.current = (response.items as unknown[]).map((item) =>
        extractId(item, resourceType),
      );
    }).catch(() => {
      // Silently fail — navigation just won't work
    });
  }, [resourceType, searchParams]);

  useHotkeys({
    "]": useCallback(() => {
      const list = listRef.current;

      if (list.length === 0) {
        return;
      }

      const index = list.indexOf(currentIdRef.current);

      if (index === -1) {
        return;
      }

      const nextIndex = (index + 1) % list.length;
      navigate(`${basePath}/${encodeURIComponent(list[nextIndex])}`);
    }, [navigate, basePath]),

    "[": useCallback(() => {
      const list = listRef.current;

      if (list.length === 0) {
        return;
      }

      const index = list.indexOf(currentIdRef.current);

      if (index === -1) {
        return;
      }

      const previousIndex = index === 0 ? list.length - 1 : index - 1;
      navigate(`${basePath}/${encodeURIComponent(list[previousIndex])}`);
    }, [navigate, basePath]),
  });
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/hooks/useResourceNavigation.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/useResourceNavigation.ts frontend/src/hooks/useResourceNavigation.test.tsx
git commit -m "feat(frontend): add prev/next resource navigation with [ and ] keys"
```

---

### Task 7: Integrate Hooks into List Pages

**Files:**
- Modify: `frontend/src/pages/NodeList.tsx`
- Modify: `frontend/src/pages/ServiceList.tsx`
- Modify: `frontend/src/pages/TaskList.tsx`
- Modify: `frontend/src/pages/StackList.tsx`
- Modify: `frontend/src/pages/ConfigList.tsx`
- Modify: `frontend/src/pages/SecretList.tsx`
- Modify: `frontend/src/pages/NetworkList.tsx`
- Modify: `frontend/src/pages/VolumeList.tsx`
- Modify: `frontend/src/components/search/SearchInput.tsx`
- Modify: `frontend/src/components/ListToolbar.tsx`
- Modify: `frontend/src/components/ViewToggle.tsx`

All 8 list pages follow the same pattern. For each:

- [ ] **Step 1: Add `ref` forwarding to SearchInput**

Modify `frontend/src/components/search/SearchInput.tsx` to forward a ref to the `<input>` element. Use `forwardRef`:

```tsx
import { Search, X } from "lucide-react";
import { forwardRef } from "react";

interface Props {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
}

const SearchInput = forwardRef<HTMLInputElement, Props>(function SearchInput(
  { value, onChange, placeholder, className },
  ref,
) {
  return (
    <div className={`relative w-full ${className}`}>
      <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />

      <input
        ref={ref}
        type="text"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder || "Search\u2026"}
        className="w-full rounded-md border bg-background py-2 pr-8 pl-9 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
      />

      {value && (
        <button
          type="button"
          aria-label="Clear search"
          onClick={() => onChange("")}
          className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-0.5 text-muted-foreground outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
});

export default SearchInput;
```

- [ ] **Step 2: Add `searchRef` to ListToolbar**

Modify `frontend/src/components/ListToolbar.tsx` to accept and forward a search input ref:

```tsx
import type { ViewMode } from "../hooks/useViewMode";
import type { RefObject } from "react";
import { SearchInput } from "./search";
import ViewToggle from "./ViewToggle";

interface Props {
  search: string;
  onSearchChange: (value: string) => void;
  placeholder?: string;
  viewMode?: ViewMode;
  onViewModeChange?: (mode: ViewMode) => void;
  searchRef?: RefObject<HTMLInputElement | null>;
}

export default function ListToolbar({
  search,
  onSearchChange,
  placeholder,
  viewMode,
  onViewModeChange,
  searchRef,
}: Props) {
  return (
    <div className="mb-4 flex items-stretch gap-3">
      <SearchInput
        ref={searchRef}
        value={search}
        onChange={onSearchChange}
        placeholder={placeholder}
      />
      {viewMode != null && onViewModeChange && (
        <div className="hidden md:block">
          <ViewToggle
            mode={viewMode}
            onChange={onViewModeChange}
          />
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 3: Add ShortcutTooltip to ViewToggle**

Modify `frontend/src/components/ViewToggle.tsx` to wrap the component with a `ShortcutTooltip`:

```tsx
import type { ViewMode } from "../hooks/useViewMode";
import ShortcutTooltip from "./ShortcutTooltip";
import { LayoutGrid, TableProperties } from "lucide-react";

interface Props {
  mode: ViewMode;
  onChange: (mode: ViewMode) => void;
}

export default function ViewToggle({ mode, onChange }: Props) {
  return (
    <ShortcutTooltip keys={["v"]}>
      <div className="inline-flex shrink-0 rounded-md border">
        <button
          onClick={() => onChange("table")}
          aria-pressed={mode === "table"}
          className="flex items-center px-2.5 py-2.5 text-muted-foreground hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground"
          title="Table view"
        >
          <TableProperties className="size-4" />
        </button>
        <button
          onClick={() => onChange("grid")}
          aria-pressed={mode === "grid"}
          className="flex items-center border-l px-2.5 py-2.5 text-muted-foreground hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground"
          title="Grid view"
        >
          <LayoutGrid className="size-4" />
        </button>
      </div>
    </ShortcutTooltip>
  );
}
```

- [ ] **Step 4: Update NodeList as the reference implementation**

Modify `frontend/src/pages/NodeList.tsx`. Add three imports and three hook calls:

Add imports:
```tsx
import { useHotkeys } from "../hooks/useHotkeys";
import { useSearchFocus } from "../hooks/useSearchFocus";
```

Add `useRef` to the existing `react` import.

Inside the `NodeList` component, add:
```tsx
const searchRef = useRef<HTMLInputElement>(null);
useSearchFocus(searchRef);
useHotkeys({
  v: useCallback(() => setViewMode(viewMode === "table" ? "grid" : "table"), [viewMode, setViewMode]),
});
```

Pass `searchRef` to `ListToolbar`:
```tsx
<ListToolbar
  search={search}
  onSearchChange={setSearch}
  placeholder="Search nodes\u2026"
  viewMode={viewMode}
  onViewModeChange={setViewMode}
  searchRef={searchRef}
/>
```

- [ ] **Step 5: Apply the same pattern to the remaining 7 list pages**

For each of ServiceList, TaskList, StackList, ConfigList, SecretList, NetworkList, VolumeList:
1. Add `useRef`, `useHotkeys`, `useSearchFocus` imports
2. Add `searchRef`, `useSearchFocus(searchRef)`, and `v` hotkey
3. Pass `searchRef` to `ListToolbar`

Each page follows the exact same pattern as NodeList — the only differences are the component name and the search placeholder text. Note: only add the `v` hotkey to pages that have a `ViewToggle` (i.e., those passing `viewMode`/`onViewModeChange` to `ListToolbar`). Pages without a view mode toggle (e.g., TaskList if it only uses table view) should skip the `v` hotkey.

- [ ] **Step 6: Run existing list page tests**

Run: `cd frontend && npx vitest run src/pages/NodeList.test.tsx src/pages/ServiceList.test.tsx src/pages/StackList.test.tsx`
Expected: PASS (existing tests should still pass)

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/search/SearchInput.tsx frontend/src/components/ListToolbar.tsx \
  frontend/src/components/ViewToggle.tsx frontend/src/pages/*List.tsx
git commit -m "feat(frontend): add Cmd+F search focus, v view toggle, and search ref to list pages"
```

---

### Task 8: Integrate Hooks into Detail Pages

**Files:**
- Modify: `frontend/src/pages/NodeDetail.tsx`
- Modify: `frontend/src/pages/ServiceDetail.tsx`
- Modify: `frontend/src/pages/TaskDetail.tsx`
- Modify: `frontend/src/pages/StackDetail.tsx`
- Modify: `frontend/src/pages/ConfigDetail.tsx`
- Modify: `frontend/src/pages/SecretDetail.tsx`
- Modify: `frontend/src/pages/NetworkDetail.tsx`
- Modify: `frontend/src/pages/VolumeDetail.tsx`

- [ ] **Step 1: Add ResourceNavIndicator to PageHeader**

Modify `frontend/src/components/PageHeader.tsx`. Add a small prev/next hint in the breadcrumb area when navigation is available. Add a new optional prop `resourceNav` to `PageHeader`:

```tsx
interface Props {
  title: React.ReactNode;
  subtitle?: string;
  breadcrumbs?: Crumb[];
  actions?: React.ReactNode;
  resourceNav?: { previous: string | null; next: string | null };
}
```

After the breadcrumbs `<nav>`, conditionally render a hint:
```tsx
{resourceNav && (
  <span className="ml-auto hidden items-center gap-2 text-xs text-muted-foreground lg:flex">
    <ShortcutTooltip keys={["["]}>
      <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono text-[10px]">[</kbd>
    </ShortcutTooltip>
    <span>prev / next</span>
    <ShortcutTooltip keys={["]"]}>
      <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono text-[10px]">]</kbd>
    </ShortcutTooltip>
  </span>
)}
```

Import `ShortcutTooltip` at the top.

- [ ] **Step 2: Update useResourceNavigation to return nav state**

The hook already returns `{ hasList }`. Extend the return to also expose previous/next resource names (for the indicator). This is optional visual context — the indicator works with just `hasList`.

- [ ] **Step 3: Update NodeDetail as the reference implementation**

Modify `frontend/src/pages/NodeDetail.tsx`. Add imports:
```tsx
import { useSectionNavigation } from "../hooks/useSectionNavigation";
import { useResourceNavigation } from "../hooks/useResourceNavigation";
```

Inside the component, after the `useParams` call:
```tsx
useSectionNavigation();
useResourceNavigation({
  resourceType: "nodes",
  currentId: id!,
  basePath: "/nodes",
});
```

Pass `resourceNav` to `PageHeader` (optional — even without it, the `[`/`]` keys work).

- [ ] **Step 4: Apply the same pattern to the remaining 7 detail pages**

For each detail page, add the two hook calls with appropriate `resourceType`, `currentId`, and `basePath`:

| Page | resourceType | currentId | basePath |
|------|-------------|-----------|----------|
| ServiceDetail | `"services"` | `id!` | `"/services"` |
| TaskDetail | `"tasks"` | `id!` | `"/tasks"` |
| StackDetail | `"stacks"` | `name!` | `"/stacks"` |
| ConfigDetail | `"configs"` | `id!` | `"/configs"` |
| SecretDetail | `"secrets"` | `id!` | `"/secrets"` |
| NetworkDetail | `"networks"` | `id!` | `"/networks"` |
| VolumeDetail | `"volumes"` | `name!` | `"/volumes"` |

Note: StackDetail and VolumeDetail use `name` from `useParams`, not `id`.

- [ ] **Step 5: Run type check to verify all integrations compile**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/PageHeader.tsx frontend/src/pages/*Detail.tsx
git commit -m "feat(frontend): add section navigation and resource prev/next to detail pages"
```

---

### Task 9: Focus Trapping — SearchPalette and ShortcutsHelp

**Files:**
- Modify: `frontend/src/components/search/SearchPalette.tsx`
- Modify: `frontend/src/components/search/GlobalSearch.tsx`
- Modify: `frontend/src/components/ShortcutsHelp.tsx`

- [ ] **Step 1: Refactor SearchPalette to use Dialog**

Modify `frontend/src/components/search/GlobalSearch.tsx` and `SearchPalette.tsx`:

In `GlobalSearch.tsx`, replace the conditional rendering with Dialog. Import the Dialog component:
```tsx
import { Dialog, DialogContent } from "../ui/dialog";
```

Create a ref for the search input and pass it to both Dialog and SearchPaletteContent:
```tsx
const inputRef = useRef<HTMLInputElement>(null);
```

Replace `{open && <SearchPalette onClose={close} />}` with:
```tsx
<Dialog open={open} onOpenChange={setOpen}>
  <DialogContent
    showCloseButton={false}
    initialFocus={() => inputRef.current}
    className="top-[5vh] left-1/2 max-w-lg -translate-x-1/2 -translate-y-0 gap-0 p-0 md:top-[15vh]"
  >
    <SearchPaletteContent onClose={close} inputRef={inputRef} />
  </DialogContent>
</Dialog>
```

The `initialFocus` prop ensures the search input receives focus when the dialog opens, replacing the previous `useEffect` auto-focus in SearchPalette.

In `SearchPalette.tsx`, remove the `createPortal` wrapper and the outer backdrop `<div className="fixed inset-0 ...">` — the Dialog provides both portal and backdrop. The component renders just its inner content (the dialog panel). Rename the export to `SearchPaletteContent` (or keep the name and update the import).

The Dialog's `modal={true}` default provides focus trapping and restoration.

- [ ] **Step 2: Refactor ShortcutsHelp to use Dialog**

Modify `frontend/src/components/ShortcutsHelp.tsx` and `frontend/src/App.tsx`:

In `App.tsx`, replace `{shortcutsOpen && <ShortcutsHelp onClose={...} />}` with:
```tsx
<Dialog open={shortcutsOpen} onOpenChange={setShortcutsOpen}>
  <ShortcutsHelp />
</Dialog>
```

In `ShortcutsHelp.tsx`, remove `createPortal`, the backdrop div, and the escape key handler. The component renders a `DialogContent` with the shortcuts content inside. Remove the `onClose` prop — Dialog handles closing.

- [ ] **Step 3: Run all frontend tests to verify nothing broke**

Run: `cd frontend && npx vitest run`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/search/SearchPalette.tsx frontend/src/components/search/GlobalSearch.tsx \
  frontend/src/components/ShortcutsHelp.tsx frontend/src/App.tsx
git commit -m "feat(frontend): refactor SearchPalette and ShortcutsHelp to Dialog for focus trapping"
```

---

### Task 10: Focus Trapping — Inline Editors

**Files:**
- Modify: `frontend/src/components/KeyValueEditor.tsx`

- [ ] **Step 1: Wrap edit mode with FocusTrap + useFocusRestore**

Modify `frontend/src/components/KeyValueEditor.tsx`:

Add imports:
```tsx
import FocusTrap from "@/components/FocusTrap";
import { useFocusRestore } from "@/hooks/useFocusRestore";
```

The editor has an `editing` state. When `editing` is true, wrap the edit-mode content (the `<div className="flex flex-col gap-3">` block around line 175) with `<FocusTrap>`. Call `useFocusRestore()` conditionally — extract the edit-mode rendering into a sub-component `EditorForm` that calls the hook on mount:

```tsx
function EditorForm({ children }: { children: ReactNode }) {
  useFocusRestore();

  return <FocusTrap>{children}</FocusTrap>;
}
```

Then in the main component, wrap the editing branch:
```tsx
{editing ? (
  <EditorForm>
    <div className="flex flex-col gap-3">
      {/* existing editor table and footer */}
    </div>
  </EditorForm>
) : (
  // existing read-only view
)}
```

- [ ] **Step 2: Add aria-label to the Edit button**

Update the pencil button in `KeyValueEditor` (around line 145) to include an `aria-label`:

```tsx
<Button
  variant="outline"
  size="xs"
  onClick={openEdit}
  aria-label={`Edit ${title.toLowerCase()}`}
>
  <Pencil className="size-3" />
  Edit
</Button>
```

- [ ] **Step 3: Run type check and existing tests**

Run: `cd frontend && npx tsc -b --noEmit && npx vitest run`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/KeyValueEditor.tsx
git commit -m "feat(frontend): add focus trapping and restoration to inline editors"
```

---

### Task 11: LogSearch Cmd+F Integration

**Files:**
- Modify: `frontend/src/components/log/LogSearch.tsx`

- [ ] **Step 1: Update LogSearch to add second-press pass-through**

The `LogSearch` component already has a Cmd+F handler (lines 32-44) that intercepts when focus is inside the log container. Update to also allow the event through when the search input is already focused:

Modify the handler in `LogSearch.tsx` (replace lines 32-44):

```tsx
const handleKeyDown = useCallback(
  (event: KeyboardEvent) => {
    if (!(event.metaKey || event.ctrlKey) || event.key !== "f") {
      return;
    }

    // If search input already focused, let browser Ctrl+F through
    if (document.activeElement === searchRef.current) {
      return;
    }

    // Only intercept when focus is inside the log container
    if (!logContainerRef.current?.contains(document.activeElement)) {
      return;
    }

    event.preventDefault();
    searchRef.current?.focus();
  },
  [logContainerRef, searchRef],
);
```

- [ ] **Step 2: Run LogViewer tests**

Run: `cd frontend && npx vitest run src/components/log/LogViewer.test.tsx`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/log/LogSearch.tsx
git commit -m "feat(frontend): add second-press Cmd+F pass-through in log search"
```

---

### Task 12: Update ShortcutsHelp with New Shortcuts

**Files:**
- Modify: `frontend/src/components/ShortcutsHelp.tsx`

- [ ] **Step 1: Add new shortcut entries to the groups array**

Update the `groups` constant in `ShortcutsHelp.tsx`. Add entries to existing groups and add a new "Detail Pages" group:

```tsx
const groups: ShortcutGroup[] = [
  {
    title: "Global",
    shortcuts: [
      { keys: ["?"], description: "Show keyboard shortcuts" },
      { keys: ["/"], description: "Focus search" },
      { keys: ["\u2318", "K"], description: "Open search palette" },
      { keys: ["\u2318", "F"], description: "Focus in-page search" },
      { keys: ["Esc"], description: "Close overlay / go back" },
      { keys: ["\u2325 hold"], description: "Show all shortcut hints" },
    ],
  },
  {
    title: "Navigation",
    shortcuts: [
      { keys: ["g", "h"], description: "Go to cluster overview" },
      { keys: ["g", "n"], description: "Go to nodes" },
      { keys: ["g", "s"], description: "Go to services" },
      { keys: ["g", "a"], description: "Go to tasks" },
      { keys: ["g", "k"], description: "Go to stacks" },
      { keys: ["g", "c"], description: "Go to configs" },
      { keys: ["g", "x"], description: "Go to secrets" },
      { keys: ["g", "w"], description: "Go to networks" },
      { keys: ["g", "v"], description: "Go to volumes" },
      { keys: ["g", "i"], description: "Go to swarm info" },
      { keys: ["g", "t"], description: "Go to topology" },
    ],
  },
  {
    title: "Lists",
    shortcuts: [
      { keys: ["j", "\u2193"], description: "Next row" },
      { keys: ["k", "\u2191"], description: "Previous row" },
      { keys: ["Enter"], description: "Open selected row" },
      { keys: ["v"], description: "Toggle table / grid view" },
    ],
  },
  {
    title: "Detail Pages",
    shortcuts: [
      { keys: ["["], description: "Previous resource" },
      { keys: ["]"], description: "Next resource" },
      { keys: ["{"], description: "Previous section" },
      { keys: ["}"], description: "Next section" },
    ],
  },
];
```

- [ ] **Step 2: Run type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ShortcutsHelp.tsx
git commit -m "feat(frontend): add new keyboard shortcuts to help modal"
```

---

### Task 13: AlertDialog Initial Focus + Tab Order Verification

**Files:**
- Verify: `frontend/src/components/ui/alert-dialog.tsx`
- Verify: `frontend/src/components/service-detail/*.tsx` (editor buttons)

- [ ] **Step 1: Add initialFocus to existing destructive AlertDialogs**

Base UI's AlertDialog enforces `modal={true}` by default, so focus trapping works. The `AlertDialogContent` component passes `{...props}` to `AlertDialogPrimitive.Popup`, so `initialFocus` can be passed by consumers directly.

Find all existing AlertDialog usages for destructive actions (e.g., in `ServiceActions`, `NodeActions`, `TaskDetail`) and add `initialFocus` pointing to the cancel button ref:

```tsx
const cancelRef = useRef<HTMLButtonElement>(null);

<AlertDialogContent initialFocus={() => cancelRef.current}>
  ...
  <AlertDialogCancel ref={cancelRef}>Cancel</AlertDialogCancel>
</AlertDialogContent>
```

This ensures keyboard users land on Cancel (the safe option) rather than the destructive action button when a confirmation dialog opens.

- [ ] **Step 2: Verify tab order on a representative detail page**

Read `frontend/src/pages/ServiceDetail.tsx` and verify that action buttons in PageHeader's `actions` slot are standard `<button>` elements rendered in visual order. Check that `NodeActions`, `ServiceActions` components render buttons without negative `tabIndex`.

- [ ] **Step 3: Verify pencil buttons have aria-label**

The `KeyValueEditor` was already updated in Task 10 to have `aria-label`. Check other editors (PortsEditor, ResourcesEditor, PlacementEditor, PolicyEditor, LogDriverEditor, HealthcheckEditor) in `frontend/src/components/service-detail/` for similar edit buttons and ensure they have `aria-label` attributes. Add `aria-label` where missing.

Note: The other service-detail editors that use `KeyValueEditor` internally (EnvEditor uses KeyValueEditor) get FocusTrap automatically.

- [ ] **Step 4: Wrap remaining service-detail editors with FocusTrap**

Editors with their own edit mode UI (PortsEditor, ResourcesEditor, PlacementEditor, PolicyEditor, LogDriverEditor, HealthcheckEditor) in `frontend/src/components/service-detail/` should be wrapped with `FocusTrap` + `useFocusRestore` following the same pattern as Task 10: extract an `EditorForm` sub-component that wraps the edit-mode content. Check each file for an `editing` state or similar toggle, and wrap the edit branch.

- [ ] **Step 5: Add ShortcutTooltip to first/last section headers**

The spec requires `{`/`}` hints on section headers visible during Alt-hold. Add `ShortcutTooltip` wrappers to the `SectionToggle` component in `CollapsibleSection.tsx`. Since all sections show the same hint, add a simple approach: the `SectionToggle` always renders inside a `ShortcutTooltip` with `keys={["{", "}"]}`. This tooltip only appears on Alt-hold (via `KeyboardHintContext`), so it won't clutter the UI during normal use.

Alternatively, only show the hint on the first visible section toggle to avoid repetition. Use the implementer's judgment on which approach feels cleaner.

- [ ] **Step 6: Fix any issues found, then commit**

```bash
git add frontend/src/components/ui/alert-dialog.tsx frontend/src/components/service-detail/ \
  frontend/src/components/CollapsibleSection.tsx frontend/src/components/node-detail/
git commit -m "fix(frontend): ensure aria-labels on editor buttons, focus trapping, and section hints"
```

---

### Task 14: Final Integration Test

- [ ] **Step 1: Run full test suite**

Run: `cd frontend && npx vitest run`
Expected: All tests pass

- [ ] **Step 2: Run lint and type check**

Run: `cd frontend && npm run lint && npx tsc -b --noEmit`
Expected: No errors

- [ ] **Step 3: Run format check**

Run: `cd frontend && npm run fmt:check`
Expected: No formatting issues (run `npm run fmt` if needed)

- [ ] **Step 4: Build the frontend**

Run: `cd frontend && npm run build`
Expected: Build succeeds

- [ ] **Step 5: Final commit if any formatting fixes were needed**

```bash
cd frontend && git add -A src/
git commit -m "chore(frontend): formatting fixes"
```
