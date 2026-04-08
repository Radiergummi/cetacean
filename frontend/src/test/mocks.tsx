import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

/**
 * Minimal EventSource mock for tests that use SSE subscriptions.
 */
export class MockEventSource {
  static instance: MockEventSource;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  closed = false;

  constructor(_url: string) {
    MockEventSource.instance = this;
  }

  addEventListener(type: string, handler: (e: MessageEvent) => void) {
    const existing = this.listeners.get(type) || [];
    existing.push(handler);
    this.listeners.set(type, existing);
  }

  close() {
    this.closed = true;
  }

  simulateEvent(type: string, data: unknown) {
    const handlers = this.listeners.get(type) || [];
    const event = new MessageEvent("message", { data: JSON.stringify(data) });
    handlers.forEach((handler) => handler(event));
  }
}

/**
 * Creates a QueryClient configured for tests (no retries, immediate GC).
 */
export function createTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
}

/**
 * Stub for localStorage that returns null for getItem and tracks
 * setItem/removeItem calls.
 */
export const localStorageStub = {
  getItem: () => null,
  setItem: vi.fn<() => void>(),
  removeItem: vi.fn<() => void>(),
};

interface CreateWrapperOptions {
  withRouter?: boolean;
}

/**
 * Creates a wrapper component for renderHook/render that provides
 * QueryClientProvider and optionally MemoryRouter.
 */
export function createWrapper(
  queryClient: QueryClient,
  options?: CreateWrapperOptions,
): ({ children }: { children: ReactNode }) => ReactNode {
  const withRouter = options?.withRouter ?? true;

  return function Wrapper({ children }: { children: ReactNode }) {
    if (withRouter) {
      return (
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <>{children}</>
          </MemoryRouter>
        </QueryClientProvider>
      );
    }

    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}
