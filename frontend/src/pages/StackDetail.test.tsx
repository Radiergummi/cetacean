import { composeQueryKey } from "../components/ComposeSection";
import StackDetail from "./StackDetail";
import { api } from "@/api/client";
import { createTestQueryClient } from "@/test/mocks";
import { QueryClientProvider } from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

interface StreamEvent {
  type: string;
  action: string;
  id: string;
}

let subscriber: ((event: StreamEvent) => void) | undefined;

vi.mock("../hooks/useResourceStream", () => ({
  useResourceStream: (_path: string, listener: (event: never) => void) => {
    subscriber = listener as (event: StreamEvent) => void;

    return { connected: true };
  },
}));

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: {
    stack: vi.fn<() => Promise<unknown>>(),
    serviceTasks: vi.fn<() => Promise<unknown>>(),
  },
}));

const stack = {
  name: "web",
  services: [
    {
      ID: "svc1",
      Spec: { Name: "web_api", Mode: { Replicated: { Replicas: 1 } } },
    },
  ],
  configs: [],
  secrets: [],
  networks: [],
  volumes: [],
};

beforeEach(() => {
  subscriber = undefined;
  vi.mocked(api.stack).mockResolvedValue({
    data: stack,
    allowedMethods: new Set<string>(),
  } as never);
  vi.mocked(api.serviceTasks).mockResolvedValue([] as never);
});

describe("StackDetail", () => {
  // A stack's document is built from its services, networks and volumes, and
  // its stream carries an event for each of them.
  it("invalidates the compose document when the stack changes", async () => {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/stacks/web"]}>
          <Routes>
            <Route
              path="/stacks/:name"
              element={<StackDetail />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(subscriber).toBeDefined();
    });
    invalidate.mockClear();

    subscriber?.({ type: "service", action: "update", id: "svc1" });

    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: [...composeQueryKey("stack:web")],
      });
    });
  });
});
