import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { SSEProvider } from "../hooks/SSEContext";
import ServiceList from "./ServiceList";
import type { Service } from "../api/types";

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
    services: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockServices = vi.mocked(api.services);

const fakeService = (id: string, name: string, replicas = 3): Service => ({
  ID: id,
  Version: { Index: 1 },
  Spec: {
    Name: name,
    Labels: {},
    TaskTemplate: { ContainerSpec: { Image: "nginx:latest@sha256:abc" } },
    Mode: { Replicated: { Replicas: replicas } },
  },
});

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    getItem: () => null,
    setItem: vi.fn(),
    removeItem: vi.fn(),
  });
  mockServices.mockReset();
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

describe("ServiceList", () => {
  it("renders service list", async () => {
    const items = [fakeService("s1", "web"), fakeService("s2", "api")];
    mockServices.mockResolvedValue({ items, total: 2 });
    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("web")).toBeInTheDocument();
    });
    expect(screen.getByText("api")).toBeInTheDocument();
    // Image without sha
    expect(screen.getAllByText("nginx:latest").length).toBeGreaterThan(0);
  });

  it("filters by search", async () => {
    mockServices
      .mockResolvedValueOnce({
        items: [fakeService("s1", "web-frontend"), fakeService("s2", "api-backend")],
        total: 2,
      })
      .mockResolvedValueOnce({
        items: [fakeService("s2", "api-backend")],
        total: 1,
      });
    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("web-frontend")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Search services..."), {
      target: { value: "api" },
    });

    await waitFor(() => {
      expect(screen.queryByText("web-frontend")).not.toBeInTheDocument();
    });
    expect(screen.getByText("api-backend")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockServices.mockResolvedValue({ items: [], total: 0 });
    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No services found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockServices.mockRejectedValue(new Error("Server error"));
    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Server error")).toBeInTheDocument();
    });
  });
});
