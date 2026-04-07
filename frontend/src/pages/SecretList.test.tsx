import type { Secret } from "../api/types";
import SecretList from "./SecretList";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

class MockEventSource {
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
}

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  setsEqual: (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x)),
  api: {
    secrets: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockSecrets = vi.mocked(api.secrets);

const fakeSecret = (id: string, name: string): Secret => ({
  ID: id,
  Version: { Index: 1 },
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-02T00:00:00Z",
  Spec: { Name: name, Labels: {} },
});

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    getItem: () => null,
    setItem: vi.fn(),
    removeItem: vi.fn(),
  });
  mockSecrets.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={testQueryClient}>
      <MemoryRouter>
        <>{children}</>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe("SecretList", () => {
  it("renders secret list", async () => {
    mockSecrets.mockResolvedValue({
      data: {
        items: [fakeSecret("s1", "db-password"), fakeSecret("s2", "api-key")],
        total: 2,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });
    render(<SecretList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("db-password")).toBeInTheDocument();
    });
    expect(screen.getByText("api-key")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockSecrets.mockResolvedValue({
      data: { items: [], total: 0, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<SecretList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No secrets found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockSecrets.mockRejectedValue(new Error("Forbidden"));
    render(<SecretList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Forbidden")).toBeInTheDocument();
    });
  });
});
