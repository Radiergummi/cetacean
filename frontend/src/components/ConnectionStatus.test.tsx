import { ConnectionProvider } from "../hooks/useResourceStream";
import ConnectionStatus from "./ConnectionStatus";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, it, expect, vi, afterEach } from "vitest";

afterEach(() => {
  vi.restoreAllMocks();
});

/**
 * Answers the /-/health poll behind useClusterFreshness. No watcher block is
 * what a server without one returns, and means "nothing to report".
 */
function stubHealth(watcher: Record<string, unknown> | null) {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () => Promise.resolve(watcher === null ? {} : { watcher }),
      } as Response),
    ),
  );
}

function createWrapper(connected: boolean, lastEventAt: number | null) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });

  return function wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <ConnectionProvider value={{ connected, lastEventAt }}>{children}</ConnectionProvider>
      </QueryClientProvider>
    );
  };
}

describe("ConnectionStatus", () => {
  it("shows 'Live' when connected", () => {
    stubHealth({ connected: true });
    render(<ConnectionStatus />, { wrapper: createWrapper(true, null) });
    expect(screen.getByText("Live")).toBeInTheDocument();
  });

  it("shows 'Reconnecting' when disconnected", () => {
    stubHealth({ connected: true });
    render(<ConnectionStatus />, { wrapper: createWrapper(false, null) });
    expect(screen.getByText("Reconnecting")).toBeInTheDocument();
  });

  it("shows 'Live' when reconnected", () => {
    stubHealth({ connected: true });
    render(<ConnectionStatus />, { wrapper: createWrapper(true, Date.now()) });
    expect(screen.getByText("Live")).toBeInTheDocument();
  });
});

describe("ConnectionStatus cluster freshness", () => {
  // Losing Docker does not disconnect the browser, so the indicator would
  // otherwise report "Live" over data that stopped moving.
  it("shows 'Stale' when the browser is connected but Cetacean is not", async () => {
    stubHealth({ connected: false, lastSyncAgeSeconds: 942 });

    render(<ConnectionStatus />, { wrapper: createWrapper(true, Date.now()) });

    await waitFor(() => {
      expect(screen.getByText("Stale")).toBeInTheDocument();
    });

    expect(screen.queryByText("Live")).not.toBeInTheDocument();
  });

  it("explains what stale means, and that it recovers on its own", async () => {
    stubHealth({ connected: false, lastSyncAgeSeconds: 942 });

    render(<ConnectionStatus />, { wrapper: createWrapper(true, Date.now()) });

    await waitFor(() => {
      expect(screen.getByText("Stale")).toBeInTheDocument();
    });

    expect(screen.getByRole("status")).toHaveAttribute(
      "title",
      expect.stringContaining("cannot reach Docker"),
    );
  });

  it("prefers 'Reconnecting' when the browser itself has lost the server", async () => {
    stubHealth({ connected: false, lastSyncAgeSeconds: 10 });

    render(<ConnectionStatus />, { wrapper: createWrapper(false, null) });

    await waitFor(() => {
      expect(screen.getByText("Reconnecting")).toBeInTheDocument();
    });

    expect(screen.queryByText("Stale")).not.toBeInTheDocument();
  });

  it("stays 'Live' against a server that reports no watcher at all", async () => {
    stubHealth(null);

    render(<ConnectionStatus />, { wrapper: createWrapper(true, null) });

    await waitFor(() => {
      expect(screen.getByText("Live")).toBeInTheDocument();
    });
  });
});
