import type { ServiceListItem } from "../api/types";
import ServiceList from "./ServiceList";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

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
  pageSize: 50,
  api: {
    services: vi.fn(),
    monitoringStatus: vi.fn().mockResolvedValue(null),
  },
}));

import { api } from "../api/client";
const mockServices = vi.mocked(api.services);

const fakeService = (id: string, name: string, replicas = 3, running = 3): ServiceListItem => ({
  ID: id,
  Version: { Index: 1 },
  Spec: {
    Name: name,
    Labels: {},
    TaskTemplate: { ContainerSpec: { Image: "nginx:latest@sha256:abc" } },
    Mode: { Replicated: { Replicas: replicas } },
  },
  RunningTasks: running,
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
      <>{children}</>
    </MemoryRouter>
  );
}

describe("ServiceList", () => {
  it("renders service list with replica health", async () => {
    const items = [fakeService("s1", "web", 3, 3), fakeService("s2", "api", 2, 1)];
    mockServices.mockResolvedValue({ data: { items, total: 2, limit: 50, offset: 0 }, allowedMethods: new Set() });
    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("web")).toBeInTheDocument();
    });
    expect(screen.getByText("api")).toBeInTheDocument();
    // Healthy: 3/3
    expect(screen.getByText("3/3")).toBeInTheDocument();
    // Unhealthy: 1/2
    expect(screen.getByText("1/2")).toBeInTheDocument();
  });

  it("filters by search with debounce", async () => {
    vi.useFakeTimers();
    mockServices
      .mockResolvedValueOnce({
        data: { items: [fakeService("s1", "web-frontend"), fakeService("s2", "api-backend")], total: 2, limit: 50, offset: 0 },
        allowedMethods: new Set(),
      })
      .mockResolvedValueOnce({
        data: { items: [fakeService("s2", "api-backend")], total: 1, limit: 50, offset: 0 },
        allowedMethods: new Set(),
      });
    render(<ServiceList />, { wrapper });

    await vi.waitFor(() => {
      expect(screen.getByText("web-frontend")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Search services…"), {
      target: { value: "api" },
    });

    // Advance past debounce
    await act(async () => {
      vi.advanceTimersByTime(300);
    });

    await vi.waitFor(() => {
      expect(screen.queryByText("web-frontend")).not.toBeInTheDocument();
    });
    expect(screen.getByText("api-backend")).toBeInTheDocument();
    vi.useRealTimers();
  });

  it("shows empty state", async () => {
    mockServices.mockResolvedValue({ data: { items: [], total: 0, limit: 50, offset: 0 }, allowedMethods: new Set() });
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
