import type { Config } from "../api/types";
import ConfigList from "./ConfigList";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
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
    configs: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockConfigs = vi.mocked(api.configs);

const fakeConfig = (id: string, name: string): Config => ({
  ID: id,
  Version: { Index: 1 },
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-02T00:00:00Z",
  Spec: { Name: name, Labels: {} },
});

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    getItem: () => null,
    setItem: vi.fn(),
    removeItem: vi.fn(),
  });
  mockConfigs.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <>{children}</>
    </MemoryRouter>
  );
}

describe("ConfigList", () => {
  it("renders config list", async () => {
    const items = [fakeConfig("c1", "app-config"), fakeConfig("c2", "db-config")];
    mockConfigs.mockResolvedValue({
      data: { items, total: 2, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<ConfigList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("app-config")).toBeInTheDocument();
    });
    expect(screen.getByText("db-config")).toBeInTheDocument();
  });

  it("filters by search", async () => {
    mockConfigs
      .mockResolvedValueOnce({
        data: {
          items: [fakeConfig("c1", "app-config"), fakeConfig("c2", "db-config")],
          total: 2,
          limit: 50,
          offset: 0,
        },
        allowedMethods: new Set(),
      })
      .mockResolvedValueOnce({
        data: { items: [fakeConfig("c2", "db-config")], total: 1, limit: 50, offset: 0 },
        allowedMethods: new Set(),
      });
    render(<ConfigList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("app-config")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Search configs\u2026"), {
      target: { value: "db" },
    });

    await waitFor(() => {
      expect(screen.queryByText("app-config")).not.toBeInTheDocument();
    });
    expect(screen.getByText("db-config")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockConfigs.mockResolvedValue({
      data: { items: [], total: 0, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<ConfigList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No configs found")).toBeInTheDocument();
    });
  });
});
