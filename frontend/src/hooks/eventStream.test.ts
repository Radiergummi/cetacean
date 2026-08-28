import { MockEventSource } from "../test/mocks";
import { openEventStream } from "./eventStream";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

beforeEach(() => {
  MockEventSource.instances = [];
  vi.stubGlobal("EventSource", MockEventSource);
  vi.useFakeTimers();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

/** The most recently constructed connection. */
function current(): MockEventSource {
  return MockEventSource.instances[MockEventSource.instances.length - 1]!;
}

describe("openEventStream", () => {
  it("dispatches named events to their listeners", () => {
    const received: unknown[] = [];
    const handle = openEventStream("/events", {
      listeners: {
        service: (event) => received.push(JSON.parse(event.data)),
      },
    });

    current().simulateEvent("service", { id: "abc" });

    expect(received).toEqual([{ id: "abc" }]);
    handle.close();
  });

  it("reports when the connection opens", () => {
    let opens = 0;
    const handle = openEventStream("/events", { listeners: {}, onOpen: () => (opens += 1) });

    current().simulateOpen();

    expect(opens).toBe(1);
    handle.close();
  });

  it("reports a disconnect", () => {
    let disconnects = 0;
    const handle = openEventStream("/events", {
      listeners: {},
      onDisconnected: () => (disconnects += 1),
    });

    current().simulateError();

    expect(disconnects).toBe(1);
    handle.close();
  });

  it("leaves the browser alone while it is retrying on its own", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    current().simulateError();
    await vi.advanceTimersByTimeAsync(60_000);

    expect(MockEventSource.instances).toHaveLength(1);
    handle.close();
  });

  it("reopens after the browser gives up for good", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    current().simulateError(true);
    await vi.advanceTimersByTimeAsync(1000);

    expect(MockEventSource.instances).toHaveLength(2);
    handle.close();
  });

  it("resubscribes its listeners on the new connection", async () => {
    const received: unknown[] = [];
    const handle = openEventStream("/events", {
      listeners: {
        service: (event) => received.push(JSON.parse(event.data)),
      },
    });

    current().simulateError(true);
    await vi.advanceTimersByTimeAsync(1000);
    current().simulateEvent("service", { id: "after-reconnect" });

    expect(received).toEqual([{ id: "after-reconnect" }]);
    handle.close();
  });

  it("backs off exponentially across repeated permanent failures", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    current().simulateError(true);
    await vi.advanceTimersByTimeAsync(1000);
    expect(MockEventSource.instances).toHaveLength(2);

    current().simulateError(true);
    await vi.advanceTimersByTimeAsync(1000);
    expect(MockEventSource.instances).toHaveLength(2);

    await vi.advanceTimersByTimeAsync(1000);
    expect(MockEventSource.instances).toHaveLength(3);

    handle.close();
  });

  it("restarts the backoff once a connection opens", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    current().simulateError(true);
    await vi.advanceTimersByTimeAsync(1000);
    current().simulateError(true);
    await vi.advanceTimersByTimeAsync(2000);
    expect(MockEventSource.instances).toHaveLength(3);

    current().simulateOpen();
    current().simulateError(true);
    await vi.advanceTimersByTimeAsync(1000);

    expect(MockEventSource.instances).toHaveLength(4);
    handle.close();
  });

  it("keeps retrying indefinitely rather than giving up", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    for (let attempt = 0; attempt < 12; attempt++) {
      current().simulateError(true);
      // eslint-disable-next-line no-await-in-loop
      await vi.advanceTimersByTimeAsync(30_000);
    }

    expect(MockEventSource.instances).toHaveLength(13);
    handle.close();
  });

  it("caps the backoff so a long outage still retries regularly", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    for (let attempt = 0; attempt < 8; attempt++) {
      current().simulateError(true);
      // eslint-disable-next-line no-await-in-loop
      await vi.advanceTimersByTimeAsync(60_000);
    }

    const opened = MockEventSource.instances.length;
    current().simulateError(true);
    await vi.advanceTimersByTimeAsync(30_000);

    expect(MockEventSource.instances).toHaveLength(opened + 1);
    handle.close();
  });

  it("closes the connection and cancels a pending retry", async () => {
    const handle = openEventStream("/events", { listeners: {} });
    const first = current();

    first.simulateError(true);
    handle.close();

    // The pending retry is cancelled outright, not merely ignored when it
    // fires — a closed stream should leave no timer behind.
    expect(vi.getTimerCount()).toBe(0);

    await vi.advanceTimersByTimeAsync(60_000);

    expect(first.closed).toBe(true);
    expect(MockEventSource.instances).toHaveLength(1);
  });
});
