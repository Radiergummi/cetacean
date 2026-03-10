import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
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
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText(/Server started/)).toBeInTheDocument();
    });
    expect(screen.getByText(/Connection failed/)).toBeInTheDocument();
  });

  it("shows error state on fetch failure", async () => {
    mockServiceLogs.mockRejectedValue(new Error("fail"));
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("Failed to load logs")).toBeInTheDocument();
    });
  });

  it("shows empty state when no logs", async () => {
    mockServiceLogs.mockResolvedValue({ lines: [], oldest: "", newest: "" });
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("No logs yet — the container hasn't produced any output")).toBeInTheDocument();
    });
  });

  it("filters logs by search", async () => {
    mockServiceLogs.mockResolvedValue(
      logResponse([{ message: "line one" }, { message: "line two" }, { message: "line three" }]),
    );
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Filter logs..."), {
      target: { value: "two" },
    });

    expect(screen.queryByText("line one")).not.toBeInTheDocument();
    expect(screen.getByText(/two/)).toBeInTheDocument();
    expect(screen.getByText("1/3")).toBeInTheDocument();
  });

  it("shows 'No matching log lines' when search has no results", async () => {
    mockServiceLogs.mockResolvedValue(logResponse([{ message: "line one" }]));
    render(<LogViewer serviceId="svc1" />);

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
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(mockServiceLogs).toHaveBeenCalledWith("svc1", expect.objectContaining({ limit: 500 }));
    });
  });

  it("re-fetches when limit changes", async () => {
    mockServiceLogs.mockResolvedValue(logResponse([{ message: "line one" }]));
    render(<LogViewer serviceId="svc1" />);

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
    render(<LogViewer serviceId="svc1" />);

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

    render(<LogViewer serviceId="svc1" />);

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
    const { unmount } = render(<LogViewer serviceId="svc1" />);

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

    render(<LogViewer serviceId="svc1" />);

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
});
