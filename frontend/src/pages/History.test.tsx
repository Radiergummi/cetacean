import type { HistoryPage } from "../api/client";
import type { HistoryEntry } from "../api/types";
import { MockEventSource, createTestQueryClient, createWrapper } from "../test/mocks";
import History from "./History";
import { QueryClient } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../api/client", async (importOriginal) => {
  const { api } = await importOriginal<typeof import("../api/client")>();

  return {
    api: {
      historyPath: api.historyPath,
      historyPage: vi.fn<() => Promise<HistoryPage>>(),
    },
  };
});

import { api } from "../api/client";
const mockHistoryPage = vi.mocked(api.historyPage);

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
  mockHistoryPage.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function renderPage() {
  render(<History />, { wrapper: createWrapper(testQueryClient) });
}

describe("History", () => {
  it("asks for the most entries one page holds", async () => {
    mockHistoryPage.mockResolvedValue({
      items: [entry(1, "service", "web"), entry(2, "node", "node-1")],
    });
    renderPage();

    expect(await screen.findByText("web")).toBeInTheDocument();
    expect(screen.getByText("node-1")).toBeInTheDocument();
    expect(mockHistoryPage).toHaveBeenCalledWith("/history?limit=200", expect.anything());
  });

  it("filters by type on the server", async () => {
    mockHistoryPage.mockResolvedValue({ items: [] });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Services" }));

    await waitFor(() =>
      expect(mockHistoryPage).toHaveBeenCalledWith(
        "/history?type=service&limit=200",
        expect.anything(),
      ),
    );
  });

  it("searches names on the server, so pages not yet loaded are searched too", async () => {
    mockHistoryPage.mockResolvedValue({ items: [entry(1, "service", "web")] });
    renderPage();

    await screen.findByText("web");
    fireEvent.change(screen.getByPlaceholderText("Filter by name…"), { target: { value: "db" } });

    await waitFor(() =>
      expect(mockHistoryPage).toHaveBeenCalledWith(
        "/history?search=db&limit=200",
        expect.anything(),
      ),
    );
  });

  it("loads older entries by following the next link", async () => {
    const older = "/history?limit=200&before=2";
    mockHistoryPage.mockImplementation(async (path) =>
      path === older
        ? { items: [entry(1, "service", "db")] }
        : { items: [entry(3, "service", "web"), entry(2, "service", "api")], next: older },
    );
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Load older events" }));

    expect(await screen.findByText("db")).toBeInTheDocument();
    expect(screen.getByText("web")).toBeInTheDocument();
    expect(mockHistoryPage).toHaveBeenCalledWith(older, expect.anything());
    expect(screen.queryByRole("button", { name: "Load older events" })).not.toBeInTheDocument();
  });
});
