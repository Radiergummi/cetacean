import { ConnectionProvider } from "../hooks/useResourceStream";
import ConnectionStatus from "./ConnectionStatus";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, it, expect, vi, afterEach } from "vitest";

afterEach(() => {
  vi.restoreAllMocks();
});

function createWrapper(connected: boolean, lastEventAt: number | null) {
  return function wrapper({ children }: { children: ReactNode }) {
    return <ConnectionProvider value={{ connected, lastEventAt }}>{children}</ConnectionProvider>;
  };
}

describe("ConnectionStatus", () => {
  it("shows 'Live' when connected", () => {
    render(<ConnectionStatus />, { wrapper: createWrapper(true, null) });
    expect(screen.getByText("Live")).toBeInTheDocument();
  });

  it("shows 'Reconnecting' when disconnected", () => {
    render(<ConnectionStatus />, { wrapper: createWrapper(false, null) });
    expect(screen.getByText("Reconnecting")).toBeInTheDocument();
  });

  it("shows 'Live' when reconnected", () => {
    render(<ConnectionStatus />, { wrapper: createWrapper(true, Date.now()) });
    expect(screen.getByText("Live")).toBeInTheDocument();
  });
});
