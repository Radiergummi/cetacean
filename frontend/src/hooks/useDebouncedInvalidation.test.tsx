import { useDebouncedInvalidation } from "./useDebouncedInvalidation";
import type { SSEEvent, SSEListener } from "./useResourceStream";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

let listener: SSEListener | undefined;

vi.mock("./useResourceStream", () => ({
  useResourceStream: (_path: string | undefined, onEvent: SSEListener) => {
    listener = onEvent;
  },
}));

const event = { type: "service", action: "update", id: "s1" } as SSEEvent;

beforeEach(() => {
  vi.useFakeTimers();
  listener = undefined;
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useDebouncedInvalidation", () => {
  it("refetches at most once per max-wait during a stream that never goes quiet", () => {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    renderHook(
      () => useDebouncedInvalidation("/services/s1", { resourceKeys: [["service", "s1"]] }),
      {
        wrapper: createWrapper(client),
      },
    );

    for (let elapsed = 0; elapsed < 12_000; elapsed += 200) {
      listener?.(event);
      vi.advanceTimersByTime(200);
    }

    expect(invalidate).toHaveBeenCalledTimes(2);
  });

  it("ignores events the caller does not count, but never a sync", () => {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    renderHook(
      () =>
        useDebouncedInvalidation("/tasks", {
          resourceKeys: [["job-outcomes"]],
          relevant: ({ id }) => id === "wanted",
        }),
      { wrapper: createWrapper(client) },
    );

    listener?.(event);
    vi.advanceTimersByTime(500);

    expect(invalidate).not.toHaveBeenCalled();

    listener?.({ type: "sync", action: "sync", id: "" });
    vi.advanceTimersByTime(500);

    expect(invalidate).toHaveBeenCalledTimes(1);
  });

  it("waits for a burst to go quiet before refetching once", () => {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    renderHook(
      () => useDebouncedInvalidation("/services/s1", { resourceKeys: [["service", "s1"]] }),
      {
        wrapper: createWrapper(client),
      },
    );

    listener?.(event);
    vi.advanceTimersByTime(300);
    listener?.(event);
    vi.advanceTimersByTime(300);

    expect(invalidate).not.toHaveBeenCalled();

    vi.advanceTimersByTime(200);

    expect(invalidate).toHaveBeenCalledTimes(1);
  });

  it("refetches only the timeline on a note or a gap", () => {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    renderHook(
      () =>
        useDebouncedInvalidation("/services/s1", {
          resourceKeys: [["service", "s1"]],
          timelineKeys: [["history", "s1"]],
        }),
      { wrapper: createWrapper(client) },
    );

    listener?.({ type: "annotation", action: "create", id: "7" });
    listener?.({ type: "gap", action: "create", id: "" });
    vi.advanceTimersByTime(500);

    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["history", "s1"] });

    invalidate.mockClear();
    listener?.({ type: "annotation", action: "create", id: "8" });
    listener?.(event);
    vi.advanceTimersByTime(500);

    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["service", "s1"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["history", "s1"] });
  });

  it("does nothing for an event of a kind it does not know", () => {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    renderHook(() => useDebouncedInvalidation("/events", { resourceKeys: [["cluster"]] }), {
      wrapper: createWrapper(client),
    });

    listener?.({ type: "annotation", action: "create", id: "7" });
    listener?.({ type: "forecast", action: "create", id: "1" });
    vi.advanceTimersByTime(500);

    expect(invalidate).not.toHaveBeenCalled();
  });
});
