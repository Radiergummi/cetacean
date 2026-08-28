import { MockEventSource } from "../test/mocks";
import { openEventStream } from "./eventStream";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/** Drives document.visibilityState, which jsdom leaves read-only. */
function setVisibility(state: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", {
    value: state,
    configurable: true,
  });
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  setVisibility("visible");
  MockEventSource.instances = [];
  vi.stubGlobal("EventSource", MockEventSource);
  vi.useFakeTimers();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

// The backoff curve: the first retry waits the interval the server asks for
// in its Retry-After, and each subsequent one doubles up to a 30s ceiling.
const firstRetryDelay = 5000;

describe("openEventStream", () => {
  it("dispatches named events to their listeners", () => {
    const received: unknown[] = [];
    const handle = openEventStream("/events", {
      listeners: {
        service: (event) => received.push(JSON.parse(event.data)),
      },
    });

    MockEventSource.instance.simulateEvent("service", { id: "abc" });

    expect(received).toEqual([{ id: "abc" }]);
    handle.close();
  });

  it("reports when the connection opens", () => {
    let opens = 0;
    const handle = openEventStream("/events", { listeners: {}, onOpen: () => (opens += 1) });

    MockEventSource.instance.simulateOpen();

    expect(opens).toBe(1);
    handle.close();
  });

  it("reports a disconnect", () => {
    let disconnects = 0;
    const handle = openEventStream("/events", {
      listeners: {},
      onDisconnected: () => (disconnects += 1),
    });

    MockEventSource.instance.simulateError();

    expect(disconnects).toBe(1);
    handle.close();
  });

  it("leaves the browser alone while it is retrying on its own", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError();
    await vi.advanceTimersByTimeAsync(60_000);

    expect(MockEventSource.instances).toHaveLength(1);
    handle.close();
  });

  it("reopens after the browser gives up for good", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(firstRetryDelay);

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

    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(firstRetryDelay);
    MockEventSource.instance.simulateEvent("service", { id: "after-reconnect" });

    expect(received).toEqual([{ id: "after-reconnect" }]);
    handle.close();
  });

  it("backs off exponentially across repeated permanent failures", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(firstRetryDelay);
    expect(MockEventSource.instances).toHaveLength(2);

    // The second wait is twice the first, so half of it is not yet enough.
    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(firstRetryDelay);
    expect(MockEventSource.instances).toHaveLength(2);

    await vi.advanceTimersByTimeAsync(firstRetryDelay);
    expect(MockEventSource.instances).toHaveLength(3);

    handle.close();
  });

  it("restarts the backoff once a connection opens", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(firstRetryDelay);
    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(firstRetryDelay * 2);
    expect(MockEventSource.instances).toHaveLength(3);

    MockEventSource.instance.simulateOpen();
    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(firstRetryDelay);

    expect(MockEventSource.instances).toHaveLength(4);
    handle.close();
  });

  it("keeps retrying indefinitely rather than giving up", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    for (let attempt = 0; attempt < 12; attempt++) {
      MockEventSource.instance.simulateError(true);
      // eslint-disable-next-line no-await-in-loop
      await vi.advanceTimersByTimeAsync(30_000);
    }

    expect(MockEventSource.instances).toHaveLength(13);
    handle.close();
  });

  it("caps the backoff so a long outage still retries regularly", async () => {
    const handle = openEventStream("/events", { listeners: {} });

    for (let attempt = 0; attempt < 8; attempt++) {
      MockEventSource.instance.simulateError(true);
      // eslint-disable-next-line no-await-in-loop
      await vi.advanceTimersByTimeAsync(60_000);
    }

    const opened = MockEventSource.instances.length;
    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(30_000);

    expect(MockEventSource.instances).toHaveLength(opened + 1);
    handle.close();
  });

  it("closes the connection and cancels a pending retry", async () => {
    const handle = openEventStream("/events", { listeners: {} });
    const first = MockEventSource.instance;

    first.simulateError(true);
    handle.close();

    // The pending retry is cancelled outright, not merely ignored when it
    // fires — a closed stream should leave no timer behind.
    expect(vi.getTimerCount()).toBe(0);

    await vi.advanceTimersByTimeAsync(60_000);

    expect(first.closed).toBe(true);
    expect(MockEventSource.instances).toHaveLength(1);
  });

  it("does not reconnect while the tab is hidden", async () => {
    setVisibility("hidden");
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(120_000);

    expect(MockEventSource.instances).toHaveLength(1);
    handle.close();
    setVisibility("visible");
  });

  it("reconnects as soon as a hidden tab is shown again", async () => {
    setVisibility("hidden");
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError(true);
    await vi.advanceTimersByTimeAsync(120_000);
    expect(MockEventSource.instances).toHaveLength(1);

    setVisibility("visible");
    await vi.advanceTimersByTimeAsync(0);

    expect(MockEventSource.instances).toHaveLength(2);
    handle.close();
  });

  it("leaves no visibility listener behind after close", async () => {
    setVisibility("hidden");
    const handle = openEventStream("/events", { listeners: {} });

    MockEventSource.instance.simulateError(true);
    handle.close();
    setVisibility("visible");
    await vi.advanceTimersByTimeAsync(120_000);

    expect(MockEventSource.instances).toHaveLength(1);
  });
});
