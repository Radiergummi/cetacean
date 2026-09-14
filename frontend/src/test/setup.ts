import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// IntersectionObserver is not available in jsdom
(globalThis as unknown as Record<string, unknown>).IntersectionObserver = vi.fn<
  () => { observe: () => void; unobserve: () => void; disconnect: () => void }
>(function () {
  return {
    observe: vi.fn<() => void>(),
    unobserve: vi.fn<() => void>(),
    disconnect: vi.fn<() => void>(),
  };
});

// React Flow measures its viewport and every node with a ResizeObserver, which
// jsdom does not implement. Without it the graph widget's tests throw before
// rendering a single vertex.
(globalThis as unknown as Record<string, unknown>).ResizeObserver = vi.fn<
  () => { observe: () => void; unobserve: () => void; disconnect: () => void }
>(function () {
  return {
    observe: vi.fn<() => void>(),
    unobserve: vi.fn<() => void>(),
    disconnect: vi.fn<() => void>(),
  };
});

// Selecting a table row scrolls it into view. jsdom implements neither
// Element.prototype.scrollIntoView nor the layout it would need to honour it.
Element.prototype.scrollIntoView = vi.fn<() => void>();

// Chart.js requires matchMedia which jsdom does not provide
Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});

afterEach(() => {
  cleanup();

  // Collapsible sections and table preferences persist here, and jsdom keeps
  // one store for every test in a file: an expanded section would otherwise
  // decide the next test's starting state. Guarded because localStorage is
  // undefined in some of the environments this suite runs in, which is what
  // let the leak pass locally and fail in CI.
  try {
    localStorage.clear();
  } catch {
    // Nothing persisted, so nothing to clear.
  }
});
