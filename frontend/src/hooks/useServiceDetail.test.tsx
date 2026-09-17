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
const streamPaths: (string | undefined)[] = [];

vi.mock("./useResourceStream", () => ({
  useResourceStream: (path: string | undefined, listener: (event: never) => void) => {
    streamPaths.push(path);
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

vi.mock("./useRecommendations", () => ({
  useRecommendations: () => ({ items: [{ targetId: "svc1" }, { targetId: "web_api" }] }),
}));
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
  vi.clearAllMocks();
  streamPaths.length = 0;
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

  it("matches recommendations against the canonical ID", async () => {
    const { result } = renderHook(() => useServiceDetail("web_api"), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    await waitFor(() => expect(result.current.service).not.toBeNull());

    expect(result.current.serviceRecommendations).toEqual([
      expect.objectContaining({ targetId: "svc1" }),
    ]);
  });

  // The identifier travels in a query parameter no redirect rewrites, so the
  // server resolves it — but only against a type, which is what must be sent.
  it("asks history for the route parameter and the type to resolve it against", async () => {
    renderHook(() => useServiceDetail("web_api"), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    await waitFor(() => expect(api.history).toHaveBeenCalled());

    expect(api.history).toHaveBeenCalledWith(
      expect.objectContaining({ resourceId: "web_api", type: "service" }),
      expect.anything(),
    );
  });

  it("subscribes to the SSE path of the canonical ID", async () => {
    renderHook(() => useServiceDetail("web_api"), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    await waitFor(() => expect(streamPaths.at(-1)).toBe("/services/svc1"));
  });

  // Both are keyed by the route parameter, so neither has anything to wait
  // for: tasks are reached by the redirect and history resolves its own name.
  it("asks for tasks and history without waiting for the first fetch", async () => {
    let settle = (): void => {};
    vi.mocked(api.service).mockReturnValue(
      new Promise((resolve) => {
        settle = () => resolve({ data: { service }, allowedMethods: new Set<string>() } as never);
      }) as never,
    );

    renderHook(() => useServiceDetail("web_api"), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    expect(api.serviceTasks).toHaveBeenCalledWith("web_api", expect.anything());
    expect(api.history).toHaveBeenCalledWith(
      expect.objectContaining({ resourceId: "web_api" }),
      expect.anything(),
    );
    expect(streamPaths.at(-1)).toBe("/services/web_api");

    settle();

    await waitFor(() => expect(streamPaths.at(-1)).toBe("/services/svc1"));
  });
});
