import type { StackSummary } from "../api/types";
import { MockEventSource, localStorageStub } from "../test/mocks";
import StackList from "./StackList";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  api: {
    stacksSummary: vi.fn<() => void>(),
  },
}));

import { api } from "../api/client";
const mockSummary = vi.mocked(api.stacksSummary);

const fakeSummary = (name: string, overrides?: Partial<StackSummary>): StackSummary => ({
  name,
  serviceCount: 2,
  configCount: 1,
  secretCount: 0,
  networkCount: 1,
  volumeCount: 0,
  desiredTasks: 3,
  tasksByState: { running: 3 },
  updatingServices: 0,
  memoryLimitBytes: 1024 * 1024 * 1024,
  memoryUsageBytes: 512 * 1024 * 1024,
  cpuLimitCores: 2,
  cpuUsagePercent: 45,
  ...overrides,
});

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
  mockSummary.mockReset();
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

describe("StackList", () => {
  it("renders stack summaries", async () => {
    mockSummary.mockResolvedValue([fakeSummary("monitoring"), fakeSummary("app")]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("monitoring")).toBeInTheDocument();
    });
    expect(screen.getByText("app")).toBeInTheDocument();
  });

  it("filters by search", async () => {
    mockSummary.mockResolvedValue([fakeSummary("monitoring"), fakeSummary("app")]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("monitoring")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Search stacks…"), {
      target: { value: "app" },
    });

    await waitFor(() => {
      expect(screen.queryByText("monitoring")).not.toBeInTheDocument();
    });
    expect(screen.getByText("app")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockSummary.mockResolvedValue([]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No stacks found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockSummary.mockRejectedValue(new Error("Failed"));
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Failed")).toBeInTheDocument();
    });
  });

  it("shows update badge when services are updating", async () => {
    mockSummary.mockResolvedValue([fakeSummary("myapp", { updatingServices: 2 })]);
    render(<StackList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Updating 2")).toBeInTheDocument();
    });
  });
});
