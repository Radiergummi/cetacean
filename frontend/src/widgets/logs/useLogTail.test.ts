import { type GetLogsResult, pollIntervalMs, useLogTail } from "./useLogTail";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * A get_logs result as the tool returns it: raw wire lines with no level field,
 * plus the cursor the next read resumes from.
 */
function result(
  messages: { timestamp: string; message: string }[],
  cursor?: string,
): GetLogsResult {
  const lines = messages.map(({ message, timestamp }) => ({
    timestamp,
    message,
    stream: "stdout" as const,
  }));

  // Omitted rather than set to undefined: the Go field is `omitempty`, so a
  // result without a cursor has no cursor key at all.
  return cursor === undefined ? { lines } : { lines, cursor };
}

/** The bridge call the hook is given, typed as the hook expects it. */
type CallTool = (name: string, args?: Record<string, unknown>) => Promise<GetLogsResult>;

describe("useLogTail", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  /** Advances timers and lets React flush the resulting state updates. */
  async function flush(milliseconds = 0) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(milliseconds);
    });
  }

  it("reads the first page with the arguments the host called the tool with", async () => {
    const callTool = vi.fn<CallTool>().mockResolvedValue(result([]));

    renderHook(() => useLogTail(callTool, { service: "svc-a", tail: 200, level: "warn" }));

    await vi.waitFor(() => {
      expect(callTool).toHaveBeenCalledWith("get_logs", {
        service: "svc-a",
        tail: 200,
        level: "warn",
      });
    });
  });

  it("polls for new lines using the cursor from the previous result", async () => {
    const callTool = vi
      .fn<CallTool>()
      .mockResolvedValueOnce(
        result(
          [{ timestamp: "2026-08-30T10:00:00Z", message: "listening on :9000" }],
          "2026-08-30T10:00:00Z",
        ),
      )
      .mockResolvedValue(result([]));

    renderHook(() => useLogTail(callTool, { service: "svc-a" }));

    await vi.waitFor(() => expect(callTool).toHaveBeenCalledTimes(1));

    await flush(pollIntervalMs);

    expect(callTool).toHaveBeenCalledWith(
      "get_logs",
      expect.objectContaining({ service: "svc-a", since: "2026-08-30T10:00:00Z" }),
    );
  });

  it("appends the lines a poll returns to the ones already shown, classifying each", async () => {
    const callTool = vi
      .fn<CallTool>()
      .mockResolvedValueOnce(
        result(
          [{ timestamp: "2026-08-30T10:00:00Z", message: "INFO started" }],
          "2026-08-30T10:00:00Z",
        ),
      )
      .mockResolvedValueOnce(
        result(
          [{ timestamp: "2026-08-30T10:00:01Z", message: "ERROR boom" }],
          "2026-08-30T10:00:01Z",
        ),
      )
      .mockResolvedValue(result([]));

    const { result: hook } = renderHook(() => useLogTail(callTool, { service: "svc-a" }));

    await vi.waitFor(() => expect(hook.current.lines).toHaveLength(1));

    await flush(pollIntervalMs);

    await vi.waitFor(() => expect(hook.current.lines).toHaveLength(2));

    expect(hook.current.lines.map(({ level }) => level)).toEqual(["info", "error"]);
    expect(hook.current.lines.map(({ index }) => index)).toEqual([0, 1]);
  });

  it("surfaces a failed read and keeps polling", async () => {
    const callTool = vi
      .fn<CallTool>()
      .mockRejectedValueOnce(new Error("service not found"))
      .mockResolvedValue(result([]));

    const { result: hook } = renderHook(() => useLogTail(callTool, { service: "svc-a" }));

    await vi.waitFor(() => expect(hook.current.error?.message).toBe("service not found"));

    await flush(pollIntervalMs);

    await vi.waitFor(() => expect(hook.current.error).toBeUndefined());
    expect(callTool).toHaveBeenCalledTimes(2);
  });

  it("reads nothing until the host has named a service", () => {
    const callTool = vi.fn<CallTool>().mockResolvedValue(result([]));

    renderHook(() => useLogTail(callTool, undefined));

    expect(callTool).not.toHaveBeenCalled();
  });
});
