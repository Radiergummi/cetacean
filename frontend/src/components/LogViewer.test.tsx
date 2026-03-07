import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import LogViewer from "./LogViewer";

// Mock scrollIntoView which jsdom doesn't support
Element.prototype.scrollIntoView = vi.fn();

// Mock the api module
vi.mock("../api/client", () => ({
  api: {
    serviceLogs: vi.fn(),
    serviceLogsStream: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockServiceLogs = vi.mocked(api.serviceLogs);
const mockServiceLogsStream = vi.mocked(api.serviceLogsStream);

beforeEach(() => {
  mockServiceLogs.mockReset();
  mockServiceLogsStream.mockReset();
});

describe("LogViewer", () => {
  it("renders log lines after fetch", async () => {
    mockServiceLogs.mockResolvedValue(
      "2024-01-01T00:00:00Z INFO Server started\n2024-01-01T00:00:01Z ERROR Connection failed",
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
    mockServiceLogs.mockResolvedValue("");
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("No logs available")).toBeInTheDocument();
    });
  });

  it("filters logs by search", async () => {
    mockServiceLogs.mockResolvedValue("line one\nline two\nline three");
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Filter logs..."), {
      target: { value: "two" },
    });

    expect(screen.queryByText("line one")).not.toBeInTheDocument();
    expect(screen.getByText(/two/)).toBeInTheDocument();
    // Shows filter count
    expect(screen.getByText("1/3")).toBeInTheDocument();
  });

  it("shows 'No matching log lines' when search has no results", async () => {
    mockServiceLogs.mockResolvedValue("line one");
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Filter logs..."), {
      target: { value: "nonexistent" },
    });

    expect(screen.getByText("No matching log lines")).toBeInTheDocument();
  });

  it("fetches with selected tail count", async () => {
    mockServiceLogs.mockResolvedValue("line one");
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(mockServiceLogs).toHaveBeenCalledWith("svc1", 500);
    });
  });

  it("re-fetches when tail count changes", async () => {
    mockServiceLogs.mockResolvedValue("line one");
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByDisplayValue("500 lines"), {
      target: { value: "100" },
    });

    await waitFor(() => {
      expect(mockServiceLogs).toHaveBeenCalledWith("svc1", 100);
    });
  });

  it("shows live tail button", async () => {
    mockServiceLogs.mockResolvedValue("line one");
    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    expect(screen.getByTitle("Live tail")).toBeInTheDocument();
  });

  it("starts streaming when live is toggled on", async () => {
    mockServiceLogs.mockResolvedValue("2024-01-01T00:00:00Z initial line");

    // Mock a stream that sends one line then closes
    const encoder = new TextEncoder();
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode("2024-01-01T00:00:01Z streamed line\n"));
        controller.close();
      },
    });
    mockServiceLogsStream.mockResolvedValue(new Response(stream, { status: 200 }));

    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText(/initial line/)).toBeInTheDocument();
    });

    fireEvent.click(screen.getByTitle("Live tail"));

    // Live indicator should appear
    expect(screen.getByText("Live")).toBeInTheDocument();

    // Streamed line should appear
    await waitFor(() => {
      expect(screen.getByText(/streamed line/)).toBeInTheDocument();
    });

    // Stream was called with since param
    expect(mockServiceLogsStream).toHaveBeenCalledWith(
      "svc1",
      expect.objectContaining({ tail: 0, since: "2024-01-01T00:00:00Z" }),
    );
  });

  it("stops streaming when live is toggled off", async () => {
    mockServiceLogs.mockResolvedValue("line one");

    // Create a stream that never closes (simulates long-running follow)
    mockServiceLogsStream.mockResolvedValue(
      new Response(new ReadableStream({ start() {} }), { status: 200 }),
    );

    render(<LogViewer serviceId="svc1" />);

    await waitFor(() => {
      expect(screen.getByText("line one")).toBeInTheDocument();
    });

    // Start live
    fireEvent.click(screen.getByTitle("Live tail"));
    expect(screen.getByText("Live")).toBeInTheDocument();

    // Stop live
    fireEvent.click(screen.getByTitle("Stop live"));
    expect(screen.queryByText("Live")).not.toBeInTheDocument();
  });
});
