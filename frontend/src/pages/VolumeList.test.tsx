import type { Volume } from "../api/types";
import VolumeList from "./VolumeList";
import { render, screen, waitFor } from "@testing-library/react";
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
    volumes: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockVolumes = vi.mocked(api.volumes);

const fakeVolume = (name: string): Volume => ({
  Name: name,
  Driver: "local",
  Labels: {},
  Mountpoint: `/var/lib/docker/volumes/${name}/_data`,
  Scope: "local",
  Options: {},
  CreatedAt: "2024-01-01T00:00:00Z",
});

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    getItem: () => null,
    setItem: vi.fn(),
    removeItem: vi.fn(),
  });
  mockVolumes.mockReset();
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

describe("VolumeList", () => {
  it("renders volume list", async () => {
    mockVolumes.mockResolvedValue({
      data: { items: [fakeVolume("db-data"), fakeVolume("cache-vol")], total: 2, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<VolumeList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("db-data")).toBeInTheDocument();
    });
    expect(screen.getByText("cache-vol")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockVolumes.mockResolvedValue({ data: { items: [], total: 0, limit: 50, offset: 0 }, allowedMethods: new Set() });
    render(<VolumeList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No volumes found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockVolumes.mockRejectedValue(new Error("Not found"));
    render(<VolumeList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Not found")).toBeInTheDocument();
    });
  });
});
