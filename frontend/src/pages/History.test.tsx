import type { HistoryEntry } from "../api/types";
import { MockEventSource, createTestQueryClient, createWrapper } from "../test/mocks";
import History from "./History";
import { QueryClient } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../api/client", () => ({
  api: { history: vi.fn<() => Promise<HistoryEntry[]>>() },
}));

import { api } from "../api/client";
const mockHistory = vi.mocked(api.history);

const entry = (id: number, type: string, name: string): HistoryEntry => ({
  id,
  type,
  name,
  action: "update",
  resourceId: `r${id}`,
  timestamp: "2026-10-10T10:00:00Z",
});

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  mockHistory.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function renderPage() {
  render(<History />, { wrapper: createWrapper(testQueryClient) });
}

describe("History", () => {
  it("asks for the most entries the endpoint returns", async () => {
    mockHistory.mockResolvedValue([entry(1, "service", "web"), entry(2, "node", "node-1")]);
    renderPage();

    expect(await screen.findByText("web")).toBeInTheDocument();
    expect(screen.getByText("node-1")).toBeInTheDocument();
    expect(mockHistory).toHaveBeenCalledWith({ limit: 200 }, expect.anything());
  });

  it("filters by type on the server", async () => {
    mockHistory.mockResolvedValue([]);
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Services" }));

    await waitFor(() =>
      expect(mockHistory).toHaveBeenCalledWith({ type: "service", limit: 200 }, expect.anything()),
    );
  });

  it("filters the loaded entries by name", async () => {
    mockHistory.mockResolvedValue([entry(1, "service", "web"), entry(2, "service", "db")]);
    renderPage();

    await screen.findByText("web");
    fireEvent.change(screen.getByPlaceholderText("Filter by name…"), { target: { value: "db" } });

    await waitFor(() => expect(screen.queryByText("web")).not.toBeInTheDocument());
    expect(screen.getByText("db")).toBeInTheDocument();
  });
});
