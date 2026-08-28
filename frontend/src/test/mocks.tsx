import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { vi } from "vitest";

/**
 * Minimal EventSource mock for tests that use SSE subscriptions.
 */
export class MockEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;

  static instance: MockEventSource;
  /** Every instance constructed since the last reset, oldest first. */
  static instances: MockEventSource[] = [];
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  closed = false;
  readyState: number = MockEventSource.CONNECTING;
  readonly url: string;

  constructor(url: string) {
    this.url = url;
    MockEventSource.instance = this;
    MockEventSource.instances.push(this);
  }

  /** Simulates the browser accepting the connection. */
  simulateOpen() {
    this.readyState = MockEventSource.OPEN;
    this.onopen?.();
  }

  /**
   * Simulates a failure. `permanent` mirrors a non-2xx response, after which
   * the browser closes the connection and stops retrying by itself.
   */
  simulateError(permanent = false) {
    this.readyState = permanent ? MockEventSource.CLOSED : MockEventSource.CONNECTING;
    this.onerror?.();
  }

  addEventListener(type: string, handler: (e: MessageEvent) => void) {
    const existing = this.listeners.get(type) || [];
    existing.push(handler);
    this.listeners.set(type, existing);
  }

  close() {
    this.closed = true;
    this.readyState = MockEventSource.CLOSED;
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

/**
 * Encodes one log SSE frame the way the backend does: an `id:` line carrying
 * the timestamp, then the JSON payload.
 */
export function encodeLogFrame(
  timestamp: string,
  message: string,
  stream: "stdout" | "stderr" = "stdout",
): string {
  const data = JSON.stringify({ timestamp, message, stream });

  return `id: ${timestamp}\ndata: ${data}\n\n`;
}

/** The keepalive comment the backend emits every 15 seconds. */
export function keepaliveFrame(): string {
  return ": keepalive\n\n";
}
