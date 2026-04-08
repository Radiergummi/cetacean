import type { Secret } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import SecretList from "./SecretList";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  setsEqual: (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x)),
  api: {
    secrets: vi.fn<() => void>(),
  },
}));

import { api } from "../api/client";
const mockSecrets = vi.mocked(api.secrets);

const fakeSecret = (id: string, name: string): Secret => ({
  ID: id,
  Version: { Index: 1 },
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-02T00:00:00Z",
  Spec: { Name: name, Labels: {} },
});

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
  mockSecrets.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return createWrapper(testQueryClient)({ children });
}

describe("SecretList", () => {
  it("renders secret list", async () => {
    mockSecrets.mockResolvedValue({
      data: {
        items: [fakeSecret("s1", "db-password"), fakeSecret("s2", "api-key")],
        total: 2,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });
    render(<SecretList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("db-password")).toBeInTheDocument();
    });
    expect(screen.getByText("api-key")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockSecrets.mockResolvedValue({
      data: { items: [], total: 0, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<SecretList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No secrets found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockSecrets.mockRejectedValue(new Error("Forbidden"));
    render(<SecretList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Forbidden")).toBeInTheDocument();
    });
  });
});
