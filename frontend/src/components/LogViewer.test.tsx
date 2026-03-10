import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import LogViewer from "./LogViewer";

// Mock scrollIntoView which jsdom doesn't support
Element.prototype.scrollIntoView = vi.fn();

// Mock the api module
vi.mock("../api/client", () => ({
  api: {
    serviceLogs: vi.fn(),
    serviceLogsStreamURL: vi.fn(),
    taskLogsStreamURL: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockServiceLogs = vi.mocked(api.serviceLogs);
const mockServiceLogsStreamURL = vi.mocked(api.serviceLogsStreamURL);

function renderWithRouter(ui: React.ReactElement, initialEntries = ["/"]) {
  return render(<MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>);
}

function logResponse(lines: { message: string; timestamp?: string; stream?: string }[]) {
  const mapped = lines.map((l) => ({
    timestamp: l.timestamp ?? "",
    message: l.message,
    stream: (l.stream ?? "stdout") as "stdout" | "stderr",
  }));
  return {
    lines: mapped,
    oldest: mapped[0]?.timestamp ?? "",
    newest: mapped[mapped.length - 1]?.timestamp ?? "",
  };
}

// Mock EventSource
class MockEventSource {
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  url: string;
  closed = false;
  static instances: MockEventSource[] = [];

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }

  close() {
    this.closed = true;
  }

  // Test helper: simulate receiving an SSE event
  emit(data: string) {
    this.onmessage?.({ data } as MessageEvent);
  }
}

beforeEach(() => {
  mockServiceLogs.mockReset();
  mockServiceLogsStreamURL.mockReset();
  MockEventSource.instances = [];
  vi.stubGlobal("EventSource", MockEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("LogViewer", () => {
  it("renders log lines after fetch", async () => {
    mockServiceLogs.mockResolvedValue(
      logResponse([
        { message: "INFO Server started", timestamp: "2024-01-01T00:00:00Z" },
        { message: "ERROR Connection failed", timestamp: "2024-01-01T00:00:01Z" },
      ]),
    );
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText(/Server started/)).toBeInTheDocument();
    });
    expect(screen.getByText(/Connection failed/)).toBeInTheDocument();
  });

  it("shows error state on fetch failure", async () => {
    mockServiceLogs.mockRejectedValue(new Error("fail"));
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("Failed to load logs")).toBeInTheDocument();
    });
  });

  it("shows empty state when no logs", async () => {
    mockServiceLogs.mockResolvedValue({ lines: [], oldest: "", newest: "" });
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("No logs yet — the container hasn't produced any output")).toBeInTheDocument();
    });
  });

  it("filters logs by search", async () => {
    mockServiceLogs.mockResolvedValue(
      logResponse([{ message: "line one" }, { message: "line two" }, { message: "line three" }]),
    );
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Filter logs..."), {
      target: { value: "two" },
    });

    expect(screen.queryByText("line one")).not.toBeInTheDocument();
    expect(screen.getByText(/two/)).toBeInTheDocument();
    expect(screen.getByText("1/1")).toBeInTheDocument();
  });

  it("navigates between search matches with Enter", async () => {
    mockServiceLogs.mockResolvedValue(
      logResponse([
        { message: "foo bar" },
        { message: "baz" },
        { message: "foo qux" },
      ]),
    );
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => expect(screen.getByText(/foo bar/)).toBeInTheDocument());

    const input = screen.getByPlaceholderText("Filter logs...");
    fireEvent.change(input, { target: { value: "foo" } });

    // Should show match count starting at 1/2
    expect(screen.getByText("1/2")).toBeInTheDocument();

    // Press Enter to go to next match
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("2/2")).toBeInTheDocument();

    // Press Enter to wrap around
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("1/2")).toBeInTheDocument();

    // Shift+Enter to go back (wraps to end)
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    expect(screen.getByText("2/2")).toBeInTheDocument();
  });

  it("shows 'No matching log lines' when search has no results", async () => {
    mockServiceLogs.mockResolvedValue(logResponse([{ message: "line one" }]));
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Filter logs..."), {
      target: { value: "nonexistent" },
    });

    expect(screen.getByText("No matching log lines")).toBeInTheDocument();
  });

  it("fetches with selected limit", async () => {
    mockServiceLogs.mockResolvedValue(logResponse([{ message: "line one" }]));
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(mockServiceLogs).toHaveBeenCalledWith("svc1", expect.objectContaining({ limit: 500 }));
    });
  });

  it("re-fetches when limit changes", async () => {
    mockServiceLogs.mockResolvedValue(logResponse([{ message: "line one" }]));
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByDisplayValue("500 lines"), {
      target: { value: "100" },
    });

    await waitFor(() => {
      expect(mockServiceLogs).toHaveBeenCalledWith("svc1", expect.objectContaining({ limit: 100 }));
    });
  });

  it("shows live tail button", async () => {
    mockServiceLogs.mockResolvedValue(logResponse([{ message: "line one" }]));
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    expect(screen.getByTitle("Live tail")).toBeInTheDocument();
  });

  it("starts SSE streaming when live is toggled on", async () => {
    mockServiceLogs.mockResolvedValue(
      logResponse([{ message: "initial line", timestamp: "2024-01-01T00:00:00Z" }]),
    );
    mockServiceLogsStreamURL.mockReturnValue(
      "/api/services/svc1/logs?after=2024-01-01T00%3A00%3A00Z",
    );

    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText(/initial line/)).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTitle("Live tail"));

    expect(screen.getByText("Live")).toBeInTheDocument();
    expect(MockEventSource.instances).toHaveLength(1);
    expect(MockEventSource.instances[0].url).toContain("/api/services/svc1/logs");

    // Simulate receiving an SSE event
    MockEventSource.instances[0].emit(
      JSON.stringify({
        timestamp: "2024-01-01T00:00:01Z",
        message: "streamed line",
        stream: "stdout",
      }),
    );

    await waitFor(() => {
      expect(screen.getByText(/streamed line/)).toBeInTheDocument();
    });
  });

  it("aborts in-flight fetch on unmount", async () => {
    let abortSignal: AbortSignal | undefined;
    mockServiceLogs.mockImplementation((_id, opts) => {
      abortSignal = opts?.signal;
      return new Promise(() => {}); // never resolves
    });
    const { unmount } = renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(abortSignal).toBeDefined();
    });
    expect(abortSignal!.aborted).toBe(false);

    unmount();
    expect(abortSignal!.aborted).toBe(true);
  });

  it("stops streaming when live is toggled off", async () => {
    mockServiceLogs.mockResolvedValue(logResponse([{ message: "line one" }]));
    mockServiceLogsStreamURL.mockReturnValue("/api/services/svc1/logs");

    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTitle("Live tail"));
    expect(screen.getByText("Live")).toBeInTheDocument();
    expect(MockEventSource.instances[0].closed).toBe(false);

    fireEvent.click(screen.getByTitle("Stop live"));
    expect(screen.queryByText("Live")).not.toBeInTheDocument();
    expect(MockEventSource.instances[0].closed).toBe(true);
  });

  it("filters logs by level", async () => {
    mockServiceLogs.mockResolvedValue(
      logResponse([
        { message: "INFO starting up" },
        { message: "ERROR something broke" },
        { message: "DEBUG verbose stuff" },
      ]),
    );
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => expect(screen.getByText(/starting up/)).toBeInTheDocument());

    fireEvent.change(screen.getByTitle("Filter by level"), { target: { value: "error" } });

    expect(screen.queryByText(/starting up/)).not.toBeInTheDocument();
    expect(screen.getByText(/something broke/)).toBeInTheDocument();
    expect(screen.queryByText(/verbose stuff/)).not.toBeInTheDocument();
  });

  it("batches rapid SSE messages into single render", async () => {
    mockServiceLogs.mockResolvedValue(
      logResponse([{ message: "initial", timestamp: "2024-01-01T00:00:00Z" }]),
    );
    mockServiceLogsStreamURL.mockReturnValue("/api/services/svc1/logs");
    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => expect(screen.getByText(/initial/)).toBeInTheDocument());

    fireEvent.click(screen.getByTitle("Live tail"));
    const es = MockEventSource.instances[0];

    for (let i = 0; i < 5; i++) {
      es.emit(
        JSON.stringify({
          timestamp: `2024-01-01T00:00:0${i + 1}Z`,
          message: `batch-${i}`,
          stream: "stdout",
        }),
      );
    }

    await waitFor(() => {
      expect(screen.getByText(/batch-4/)).toBeInTheDocument();
    });
    expect(screen.getByText(/batch-0/)).toBeInTheDocument();
  });

  it("loads older logs when scrolling to top", async () => {
    // Generate exactly 100 lines so hasOlderLogs is true (100 >= limit after switching to 100)
    const initialLines = Array.from({ length: 100 }, (_, i) => ({
      message: `line ${i}`,
      timestamp: `2024-01-01T00:0${Math.floor(i / 60)}:${String(i % 60).padStart(2, "0")}Z`,
    }));
    mockServiceLogs
      .mockResolvedValueOnce(logResponse([{ message: "placeholder" }])) // initial fetch with limit=500
      .mockResolvedValueOnce(logResponse(initialLines)) // re-fetch after limit change to 100
      .mockResolvedValueOnce(
        logResponse([
          { message: "older line", timestamp: "2024-01-01T00:00:01Z" },
        ]),
      );

    renderWithRouter(<LogViewer serviceId="svc1" />);

    // Wait for initial fetch, then change limit to 100
    await waitFor(() => expect(screen.getByText("placeholder")).toBeInTheDocument());
    fireEvent.change(screen.getByDisplayValue("500 lines"), { target: { value: "100" } });

    await waitFor(() => expect(screen.getByText("line 0")).toBeInTheDocument());

    // Simulate scroll to top
    const container = document.querySelector(".log-panel")!;
    Object.defineProperty(container, "scrollTop", { value: 0, writable: true, configurable: true });
    Object.defineProperty(container, "scrollHeight", { value: 5000, writable: true, configurable: true });
    Object.defineProperty(container, "clientHeight", { value: 400, writable: true, configurable: true });
    fireEvent.scroll(container);

    await waitFor(() => {
      expect(mockServiceLogs).toHaveBeenCalledWith(
        "svc1",
        expect.objectContaining({ before: initialLines[0].timestamp }),
      );
    });

    await waitFor(() => {
      expect(screen.getByText("older line")).toBeInTheDocument();
    });
  });

  it("loads newer logs when scrolling to bottom in non-live mode", async () => {
    mockServiceLogs
      .mockResolvedValueOnce(
        logResponse([
          { message: "line 1", timestamp: "2024-01-01T00:00:01Z" },
          { message: "line 2", timestamp: "2024-01-01T00:00:02Z" },
        ]),
      )
      .mockResolvedValueOnce(
        logResponse([
          { message: "newer line", timestamp: "2024-01-01T00:00:03Z" },
        ]),
      );

    renderWithRouter(<LogViewer serviceId="svc1" />);

    await waitFor(() => expect(screen.getByText("line 2")).toBeInTheDocument());

    // Simulate scroll to bottom
    const container = document.querySelector(".log-panel")!;
    Object.defineProperty(container, "scrollTop", { value: 450, writable: true, configurable: true });
    Object.defineProperty(container, "scrollHeight", { value: 500, writable: true, configurable: true });
    Object.defineProperty(container, "clientHeight", { value: 400, writable: true, configurable: true });
    fireEvent.scroll(container);

    await waitFor(() => {
      expect(mockServiceLogs).toHaveBeenCalledWith(
        "svc1",
        expect.objectContaining({ after: "2024-01-01T00:00:02Z" }),
      );
    });

    await waitFor(() => {
      expect(screen.getByText("newer line")).toBeInTheDocument();
    });
  });

  it("reads time range from URL on mount", async () => {
    mockServiceLogs.mockResolvedValue(logResponse([{ message: "line" }]));
    renderWithRouter(<LogViewer serviceId="svc1" />, ["/?logRange=5m"]);

    await waitFor(() => {
      expect(mockServiceLogs).toHaveBeenCalledWith(
        "svc1",
        expect.objectContaining({ after: expect.any(String) }),
      );
    });
  });
});
