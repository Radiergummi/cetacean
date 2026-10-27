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
  it("refetches during a stream of events that never goes quiet", () => {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    renderHook(() => useDebouncedInvalidation("/services/s1", [["service", "s1"]], 500), {
      wrapper: createWrapper(client),
    });

    for (let elapsed = 0; elapsed < 3000; elapsed += 200) {
      listener?.(event);
      vi.advanceTimersByTime(200);
    }

    expect(invalidate.mock.calls.length).toBeGreaterThanOrEqual(5);
  });

  it("coalesces a burst into one refetch", () => {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    renderHook(() => useDebouncedInvalidation("/services/s1", [["service", "s1"]], 500), {
      wrapper: createWrapper(client),
    });

    listener?.(event);
    listener?.(event);
    listener?.(event);
    vi.advanceTimersByTime(500);

    expect(invalidate).toHaveBeenCalledTimes(1);
  });
});
