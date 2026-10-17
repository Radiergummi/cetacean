import { useResourceStream } from "../hooks/useResourceStream";
import type { SSEEvent } from "../hooks/useResourceStream";
import { MockEventSource } from "../test/mocks";
import { ConnectionTracker } from "./ConnectionTracker";
import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

function Subscriber({ path, received }: { path: string; received: SSEEvent[] }) {
  useResourceStream(path, (event) => received.push(event));

  return null;
}

beforeEach(() => {
  MockEventSource.instances = [];
  vi.stubGlobal("EventSource", MockEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ConnectionTracker", () => {
  it("serves every /events subscriber from its one stream", () => {
    const first: SSEEvent[] = [];
    const second: SSEEvent[] = [];

    render(
      <ConnectionTracker>
        <Subscriber
          path="/events"
          received={first}
        />
        <Subscriber
          path="/events"
          received={second}
        />
      </ConnectionTracker>,
    );

    expect(MockEventSource.instances).toHaveLength(1);

    act(() => {
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "update",
        id: "a",
      });
      MockEventSource.instance.simulateEvent("batch", [
        { type: "task", action: "update", id: "b" },
        { type: "node", action: "update", id: "c" },
      ]);
    });

    expect(first.map(({ id }) => id)).toEqual(["a", "b", "c"]);
    expect(second).toEqual(first);
  });

  it("stops delivering to a subscriber once it unmounts", () => {
    const received: SSEEvent[] = [];
    const { rerender } = render(
      <ConnectionTracker>
        <Subscriber
          path="/events"
          received={received}
        />
      </ConnectionTracker>,
    );

    rerender(<ConnectionTracker>{null}</ConnectionTracker>);

    act(() => {
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "update",
        id: "a",
      });
    });

    expect(received).toEqual([]);
    expect(MockEventSource.instance.closed).toBe(false);
  });

  it("opens its own stream for any other path", () => {
    render(
      <ConnectionTracker>
        <Subscriber
          path="/services"
          received={[]}
        />
      </ConnectionTracker>,
    );

    expect(MockEventSource.instances).toHaveLength(2);
  });

  it("tells subscribers to resync when it reopens the stream itself", () => {
    vi.useFakeTimers();
    const received: SSEEvent[] = [];

    render(
      <ConnectionTracker>
        <Subscriber
          path="/events"
          received={received}
        />
      </ConnectionTracker>,
    );

    act(() => {
      MockEventSource.instance.simulateOpen();
      MockEventSource.instance.simulateError(true);
      vi.runOnlyPendingTimers();
      MockEventSource.instance.simulateOpen();
    });

    vi.useRealTimers();

    expect(received).toEqual([{ type: "sync", action: "sync", id: "" }]);
  });
});

describe("useResourceStream without a tracker", () => {
  it("opens its own /events stream", () => {
    const received: SSEEvent[] = [];

    render(
      <Subscriber
        path="/events"
        received={received}
      />,
    );

    expect(MockEventSource.instances).toHaveLength(1);

    act(() => {
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "update",
        id: "a",
      });
    });

    expect(received).toHaveLength(1);
  });
});
