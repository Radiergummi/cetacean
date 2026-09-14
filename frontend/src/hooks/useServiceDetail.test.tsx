import { composeQueryKey } from "../components/ComposeSection";
import { useServiceDetail } from "./useServiceDetail";
import { api } from "@/api/client";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

interface StreamEvent {
  type: string;
  action: string;
  id: string;
}

let subscriber: ((event: StreamEvent) => void) | undefined;

vi.mock("./useResourceStream", () => ({
  useResourceStream: (_path: string, listener: (event: never) => void) => {
    subscriber = listener as (event: StreamEvent) => void;

    return { connected: true };
  },
}));

function emit(event: StreamEvent) {
  if (!subscriber) {
    throw new Error("the hook subscribed to no stream");
  }

  subscriber(event);
}

vi.mock("./useMonitoringStatus", () => ({
  useMonitoringStatus: () => ({}),
  isPrometheusReady: () => false,
  isCadvisorReady: () => false,
}));

vi.mock("./useRecommendations", () => ({ useRecommendations: () => ({ items: [] }) }));
vi.mock("./useTaskMetrics", () => ({ useTaskMetrics: () => ({}) }));

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: {
    service: vi.fn<() => Promise<unknown>>(),
    serviceTasks: vi.fn<() => Promise<unknown>>(),
    history: vi.fn<() => Promise<unknown>>(),
    networks: vi.fn<() => Promise<unknown>>(),
    metricsQuery: vi.fn<() => Promise<unknown>>(),
  },
}));

const service = {
  ID: "svc1",
  Version: { Index: 1 },
  Spec: {
    Name: "web_api",
    Mode: { Replicated: { Replicas: 1 } },
    TaskTemplate: { ContainerSpec: { Image: "nginx:1.27" } },
  },
};

beforeEach(() => {
  vi.mocked(api.service).mockResolvedValue({
    data: { service },
    allowedMethods: new Set<string>(),
  } as never);
  vi.mocked(api.serviceTasks).mockResolvedValue([] as never);
  vi.mocked(api.history).mockResolvedValue([] as never);
  vi.mocked(api.networks).mockResolvedValue({ data: { items: [] } } as never);
  vi.mocked(api.metricsQuery).mockResolvedValue({} as never);
});

describe("useServiceDetail", () => {
  async function renderWithStream() {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    renderHook(() => useServiceDetail("svc1"), { wrapper: createWrapper(client) });
    await waitFor(() => {
      expect(subscriber).toBeDefined();
    });
    invalidate.mockClear();

    return invalidate;
  }

  const composeKey = { queryKey: [...composeQueryKey("service:svc1")] };

  // The document the section is showing was rendered from the old spec.
  it("invalidates the compose document when the service changes", async () => {
    const invalidate = await renderWithStream();

    emit({ type: "service", action: "update", id: "svc1" });

    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith(composeKey);
    });
  });

  // Tasks churn on every restart, and none of it reaches the projection.
  it("leaves the compose document alone on a task event", async () => {
    const invalidate = await renderWithStream();

    emit({ type: "task", action: "update", id: "task1" });

    expect(invalidate).not.toHaveBeenCalledWith(composeKey);
  });
});
