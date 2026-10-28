import { composeQueryKey } from "../components/ComposeSection";
import { useServiceDetail } from "./useServiceDetail";
import { api } from "@/api/client";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

interface StreamEvent {
  type: string;
  action: string;
  id: string;
  resource?: unknown | undefined;
}

let subscriber: ((event: StreamEvent) => void) | undefined;
const streamPaths: (string | undefined)[] = [];

vi.mock("./useResourceStream", () => ({
  useResourceStream: (path: string | undefined, listener: (event: never) => void) => {
    streamPaths.push(path);
    subscriber = listener as (event: StreamEvent) => void;
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
    allNetworks: vi.fn<() => Promise<unknown>>(),
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
    data: { service, changes: [{ path: "Image" }], integrations: [{ kind: "traefik" }] },
    allowedMethods: new Set<string>(),
    canonicalPath: "/services/svc1",
  } as never);
  vi.mocked(api.serviceTasks).mockResolvedValue([] as never);
  vi.mocked(api.history).mockResolvedValue([] as never);
  vi.mocked(api.allNetworks).mockResolvedValue([] as never);
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
        settle = () =>
          resolve({
            data: { service },
            allowedMethods: new Set<string>(),
            canonicalPath: "/services/svc1",
          } as never);
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
  // A service event carries the service alone. Writing it into the cache as
  // the whole detail would drop the spec changes and integrations that came
  // with the fetch, and nothing on the page would say where they went.
  it("keeps changes and integrations when an event carries only the service", async () => {
    const { result } = renderHook(() => useServiceDetail("web_api"), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    await waitFor(() => expect(result.current.service).not.toBeNull());
    expect(result.current.changes).toHaveLength(1);

    act(() => {
      subscriber?.({
        type: "service",
        action: "update",
        id: "svc1",
        resource: { ...service, Spec: { ...service.Spec, Name: "renamed" } },
      });
    });

    await waitFor(() => expect(result.current.service?.Spec.Name).toBe("renamed"));

    expect(result.current.changes).toHaveLength(1);
    expect(result.current.integrations).toHaveLength(1);
  });

  it("refetches the detail when an event changes the labels the integrations are read from", async () => {
    const { result } = renderHook(() => useServiceDetail("svc1"), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    await waitFor(() => expect(result.current.service).not.toBeNull());
    const fetches = vi.mocked(api.service).mock.calls.length;

    act(() => {
      subscriber?.({
        type: "service",
        action: "update",
        id: "svc1",
        resource: { ...service, Spec: { ...service.Spec, Name: "renamed" } },
      });
    });

    await waitFor(() => expect(result.current.service?.Spec.Name).toBe("renamed"));
    expect(api.service).toHaveBeenCalledTimes(fetches);

    act(() => {
      subscriber?.({
        type: "service",
        action: "update",
        id: "svc1",
        resource: { ...service, Spec: { ...service.Spec, Labels: { "caddy.encode": "gzip" } } },
      });
    });

    await waitFor(() => expect(api.service).toHaveBeenCalledTimes(fetches + 1));
  });

  // A save is written onto the cached service, so a stream event that follows
  // re-derives from the saved spec rather than reverting the editor.
  it("writes a saved sub-resource into the detail cache", async () => {
    const client = createTestQueryClient();
    const { result } = renderHook(() => useServiceDetail("svc1"), {
      wrapper: createWrapper(client),
    });

    await waitFor(() => expect(result.current.service).not.toBeNull());

    act(() => {
      result.current.onEnvSaved({ MODE: "production" });
      result.current.onPortsSaved([{ TargetPort: 80, PublishedPort: 8080 }]);
      result.current.onHealthcheckSaved(null);
    });

    await waitFor(() => expect(result.current.envVars).toEqual({ MODE: "production" }));
    expect(result.current.specPorts).toEqual([{ TargetPort: 80, PublishedPort: 8080 }]);
    expect(result.current.healthcheck).toBeNull();

    const cached = client.getQueryData<{ data: { service: typeof service } }>([
      "detail",
      "/services/svc1",
    ]);

    expect(cached?.data.service.Spec.TaskTemplate.ContainerSpec).toEqual(
      expect.objectContaining({ Image: "nginx:1.27", Env: ["MODE=production"] }),
    );
  });

  // The server's cache trails the save, so asking it at once would hand back
  // the old labels; the stream's echo of the save is the earliest safe moment.
  it("refetches the detail on the stream's echo of a labels save, not before", async () => {
    const { result } = renderHook(() => useServiceDetail("svc1"), {
      wrapper: createWrapper(createTestQueryClient()),
    });

    await waitFor(() => expect(result.current.service).not.toBeNull());
    const fetches = vi.mocked(api.service).mock.calls.length;
    const labels = { "caddy.encode": "gzip" };
    const saved = { ...service, Version: { Index: 2 }, Spec: { ...service.Spec, Labels: labels } };

    act(() => {
      result.current.onLabelsSaved(labels);
    });

    await waitFor(() => expect(result.current.serviceLabels).toEqual(labels));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(api.service).toHaveBeenCalledTimes(fetches);

    vi.mocked(api.service).mockResolvedValue({
      data: { service: saved, integrations: [{ kind: "caddy" }] },
      allowedMethods: new Set<string>(),
      canonicalPath: "/services/svc1",
    } as never);

    act(() => {
      emit({ type: "service", action: "update", id: "svc1", resource: saved });
    });

    // Sooner than the stream's debounced invalidation would ask.
    await waitFor(() => expect(api.service).toHaveBeenCalledTimes(fetches + 1), { timeout: 300 });
    await waitFor(() =>
      expect(result.current.integrations).toEqual([expect.objectContaining({ kind: "caddy" })]),
    );
    expect(result.current.serviceLabels).toEqual(labels);
  });

  // A save on one service says nothing about the next one the page shows.
  it("forgets a pending labels echo when the page moves to another service", async () => {
    const { result, rerender } = renderHook(({ id }) => useServiceDetail(id), {
      initialProps: { id: "svc1" },
      wrapper: createWrapper(createTestQueryClient()),
    });

    await waitFor(() => expect(result.current.service).not.toBeNull());

    act(() => {
      result.current.onLabelsSaved({ "caddy.encode": "gzip" });
    });

    const other = { ...service, ID: "svc2", Spec: { ...service.Spec, Name: "other" } };

    vi.mocked(api.service).mockResolvedValue({
      data: { service: other },
      allowedMethods: new Set<string>(),
      canonicalPath: "/services/svc2",
    } as never);
    rerender({ id: "svc2" });

    await waitFor(() => expect(result.current.service?.ID).toBe("svc2"));
    const fetches = vi.mocked(api.service).mock.calls.length;

    act(() => {
      emit({
        type: "service",
        action: "update",
        id: "svc2",
        resource: { ...other, Version: { Index: 5 } },
      });
    });

    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(api.service).toHaveBeenCalledTimes(fetches);
  });
});
