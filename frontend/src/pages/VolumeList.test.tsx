import type { Volume } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import VolumeList from "./VolumeList";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  setsEqual: (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x)),
  api: {
    volumes: vi.fn<() => void>(),
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

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
  mockVolumes.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return createWrapper(testQueryClient)({ children });
}

describe("VolumeList", () => {
  it("renders volume list", async () => {
    mockVolumes.mockResolvedValue({
      data: {
        items: [fakeVolume("db-data"), fakeVolume("cache-vol")],
        total: 2,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });
    render(<VolumeList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("db-data")).toBeInTheDocument();
    });
    expect(screen.getByText("cache-vol")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockVolumes.mockResolvedValue({
      data: { items: [], total: 0, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
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
