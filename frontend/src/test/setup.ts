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
});
