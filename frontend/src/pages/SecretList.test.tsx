import type { SecretListItem } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import SecretList from "./SecretList";
import { QueryClient } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

const fakeSecret = (id: string, name: string): SecretListItem => ({
  ID: id,
  Version: { Index: 1 },
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-02T00:00:00Z",
  Spec: { Name: name, Labels: {} },
  UsedBy: 0,
});

const version = (
  id: string,
  createdAt: string,
  usedBy: number,
  versionInferred: boolean,
): SecretListItem => ({
  ...fakeSecret(id, id),
  CreatedAt: createdAt,
  UsedBy: usedBy,
  VersionOf: "db",
  VersionInferred: versionInferred,
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

  it("collapses versions into the newest, with the older ones a toggle away", async () => {
    mockSecrets.mockResolvedValue({
      data: {
        items: [
          version("db-v1", "2024-01-01T00:00:00Z", 0, true),
          version("db-v3", "2024-01-03T00:00:00Z", 2, true),
          version("db-v2", "2024-01-02T00:00:00Z", 1, true),
        ],
        total: 3,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });
    render(<SecretList />, { wrapper });

    const toggle = await screen.findByRole("button", { name: "+2 older" });

    expect(screen.getByText("db-v3")).toBeInTheDocument();
    expect(screen.queryByText("db-v1")).not.toBeInTheDocument();
    expect(screen.getByText("inferred from names")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Used by" })).toBeInTheDocument();

    fireEvent.click(toggle);

    expect(await screen.findByText("db-v1")).toBeInTheDocument();
    expect(screen.getByText("db-v2")).toBeInTheDocument();

    const rows = screen.getAllByRole("row").map(({ textContent }) => textContent ?? "");

    expect(rows.findIndex((text) => text.includes("db-v2"))).toBeLessThan(
      rows.findIndex((text) => text.includes("db-v1")),
    );
    expect(rows.find((text) => text.includes("db-v1"))).toContain("Unused");
    expect(rows.find((text) => text.includes("db-v2"))).not.toContain("Unused");
  });

  it("does not call a version grouped by label inferred", async () => {
    mockSecrets.mockResolvedValue({
      data: {
        items: [
          version("db-v1", "2024-01-01T00:00:00Z", 0, false),
          version("db-v2", "2024-01-02T00:00:00Z", 1, false),
        ],
        total: 2,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });
    render(<SecretList />, { wrapper });

    await screen.findByRole("button", { name: "+1 older" });

    expect(screen.queryByText("inferred from names")).not.toBeInTheDocument();
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
