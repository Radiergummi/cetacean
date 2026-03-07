import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { SSEProvider } from "../hooks/SSEContext";
import ConnectionStatus from "./ConnectionStatus";

class MockEventSource {
  static instance: MockEventSource;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  closed = false;

  constructor(_url: string) {
    MockEventSource.instance = this;
  }
  addEventListener(type: string, handler: (e: MessageEvent) => void) {
    const existing = this.listeners.get(type) || [];
    existing.push(handler);
    this.listeners.set(type, existing);
  }
  close() {
    this.closed = true;
  }
  simulateOpen() {
    this.onopen?.();
  }
  simulateError() {
    this.onerror?.();
  }
}

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: ReactNode }) {
  return <SSEProvider>{children}</SSEProvider>;
}

describe("ConnectionStatus", () => {
  it("shows 'Live' when connected", () => {
    render(<ConnectionStatus />, { wrapper });
    expect(screen.getByText("Live")).toBeInTheDocument();
  });

  it("shows 'Reconnecting' on error", () => {
    render(<ConnectionStatus />, { wrapper });
    act(() => MockEventSource.instance.simulateError());
    expect(screen.getByText("Reconnecting")).toBeInTheDocument();
  });

  it("recovers to 'Live' on reconnect", () => {
    render(<ConnectionStatus />, { wrapper });
    act(() => MockEventSource.instance.simulateError());
    act(() => MockEventSource.instance.simulateOpen());
    expect(screen.getByText("Live")).toBeInTheDocument();
  });
});
