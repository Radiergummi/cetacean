import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { SSEProvider } from "../hooks/SSEContext";
import StackList from "./StackList";
import type { Stack } from "../api/types";

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
}

vi.mock("../api/client", () => ({
  api: {
    stacks: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockStacks = vi.mocked(api.stacks);

const fakeStack = (name: string): Stack => ({
  name,
  services: ["svc1"],
  configs: ["cfg1"],
  secrets: [],
  networks: ["net1"],
  volumes: [],
});

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    getItem: () => null,
    setItem: vi.fn(),
    removeItem: vi.fn(),
  });
  mockStacks.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <SSEProvider>{children}</SSEProvider>
    </MemoryRouter>
  );
}

describe("StackList", () => {
  it("renders stack list", async () => {
    mockStacks.mockResolvedValue([fakeStack("monitoring"), fakeStack("app")]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("monitoring")).toBeInTheDocument();
    });
    expect(screen.getByText("app")).toBeInTheDocument();
  });

  it("filters by search", async () => {
    mockStacks.mockResolvedValue([fakeStack("monitoring"), fakeStack("app")]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("monitoring")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Search stacks..."), {
      target: { value: "app" },
    });

    expect(screen.queryByText("monitoring")).not.toBeInTheDocument();
    expect(screen.getByText("app")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockStacks.mockResolvedValue([]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No stacks found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockStacks.mockRejectedValue(new Error("Failed"));
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Failed")).toBeInTheDocument();
    });
  });
});
