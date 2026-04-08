import type { Config } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import ConfigList from "./ConfigList";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  setsEqual: (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x)),
  api: {
    configs: vi.fn<() => void>(),
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

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
  mockConfigs.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return createWrapper(testQueryClient)({ children });
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
