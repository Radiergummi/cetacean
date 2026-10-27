import type { SecretListItem } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import SecretList from "./SecretList";
import { QueryClient } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  setsEqual: (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x)),
  api: {
    secrets: vi.fn<() => void>(),
    secretVersions: vi.fn<() => void>(),
  },
}));

import { api } from "../api/client";
const mockSecrets = vi.mocked(api.secrets);
const mockSecretVersions = vi.mocked(api.secretVersions);

const fakeSecret = (id: string, name: string): SecretListItem => ({
  ID: id,
  Version: { Index: 1 },
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-02T00:00:00Z",
  Spec: { Name: name, Labels: {} },
  UsedBy: 0,
});

const version = (id: string, usedBy: number, versionInferred: boolean): SecretListItem => ({
  ...fakeSecret(id, id),
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
  mockSecretVersions.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

/**
 * Sends an event down every open stream: the list and an expanded group each
 * hold one when no shared stream is mounted.
 */
function broadcast(event: Record<string, unknown>) {
  for (const stream of MockEventSource.instances) {
    stream.simulateEvent("secret", event);
  }
}

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

  it("loads a group's older versions beneath its newest on request", async () => {
    const head = { ...version("db-v3", 2, true), NewestVersion: true, OlderVersions: 2 };

    mockSecrets.mockResolvedValue({
      data: { items: [head, fakeSecret("s9", "api-key")], total: 2, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    mockSecretVersions.mockResolvedValue([
      head,
      version("db-v2", 1, true),
      version("db-v1", 0, true),
    ]);
    render(<SecretList />, { wrapper });

    const toggle = await screen.findByRole("button", { name: "+2 older" });

    expect(screen.getByText("inferred from names")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Used by" })).toBeInTheDocument();
    expect(screen.queryByText("db-v1")).not.toBeInTheDocument();

    const streams = MockEventSource.instances.length;

    fireEvent.click(toggle);

    expect(await screen.findByText("db-v1")).toBeInTheDocument();
    // The expansion follows the list's stream rather than opening its own.
    expect(MockEventSource.instances.length).toBe(streams);
    expect(mockSecretVersions).toHaveBeenCalledWith("db-v3", expect.anything());

    const rows = screen.getAllByRole("row").map(({ textContent }) => textContent ?? "");
    const position = (name: string) => rows.findIndex((text) => text.includes(name));

    expect(position("db-v3")).toBeLessThan(position("db-v2"));
    expect(position("db-v2")).toBeLessThan(position("db-v1"));
    expect(position("db-v1")).toBeLessThan(position("api-key"));
    expect(rows[position("db-v1")]).toContain("No services you can see");
    expect(rows[position("db-v2")]).not.toContain("No services you can see");

    // An expanded group follows the same events the list does.
    const calls = mockSecretVersions.mock.calls.length;

    act(() =>
      MockEventSource.instance.simulateEvent("secret", {
        type: "secret",
        action: "create",
        id: "db-v4",
        resource: { ID: "db-v4" },
      }),
    );

    await waitFor(() => expect(mockSecretVersions.mock.calls.length).toBeGreaterThan(calls), {
      timeout: 2000,
    });
  });

  // A service gaining or losing a secret, in its spec or its previous one,
  // reaches the list as that secret's ref_changed; no service stream is needed.
  it("refetches when a reference to a secret changes", async () => {
    mockSecrets.mockResolvedValue({
      data: { items: [fakeSecret("s1", "db-password")], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<SecretList />, { wrapper });

    await screen.findByText("db-password");

    const calls = mockSecrets.mock.calls.length;

    act(() =>
      MockEventSource.instance.simulateEvent("secret", {
        type: "secret",
        action: "ref_changed",
        id: "s1",
      }),
    );

    await waitFor(() => expect(mockSecrets.mock.calls.length).toBeGreaterThan(calls), {
      timeout: 2000,
    });
  });

  it("keeps a group open under its new newest version, and closes it once gone", async () => {
    const head = (id: string) => ({
      ...version(id, 0, true),
      NewestVersion: true,
      OlderVersions: 1,
    });
    const page = (items: SecretListItem[]) => ({
      data: { items, total: items.length, limit: 50, offset: 0 },
      allowedMethods: new Set<string>(),
    });

    mockSecrets.mockResolvedValue(page([head("db-v2")]));
    mockSecretVersions.mockResolvedValue([version("db-v1", 0, true)]);
    render(<SecretList />, { wrapper });

    fireEvent.click(await screen.findByRole("button", { name: "+1 older" }));
    await screen.findByText("db-v1");

    // db-v3 arrives and heads the group.
    mockSecrets.mockResolvedValue(page([head("db-v3")]));
    act(() =>
      broadcast({
        type: "secret",
        action: "create",
        id: "db-v3",
        resource: { ID: "db-v3" },
      }),
    );

    await waitFor(
      () => expect(mockSecretVersions).toHaveBeenCalledWith("db-v3", expect.anything()),
      {
        timeout: 2000,
      },
    );
    expect(screen.getByRole("button", { name: "Hide older" })).toBeInTheDocument();

    // The group is gone from the list, so nothing keeps asking for it.
    mockSecrets.mockResolvedValue(page([fakeSecret("s9", "api-key")]));
    act(() =>
      broadcast({
        type: "secret",
        action: "remove",
        id: "db-v3",
        resource: { ID: "db-v3" },
      }),
    );

    await screen.findByText("api-key", undefined, { timeout: 2000 });
    expect(screen.queryByText("db-v1")).not.toBeInTheDocument();
  });

  // A removed head leaves the group headless until the list refetches; it
  // stays open through that and follows the version promoted in its place.
  it("keeps a group open while its removed head is replaced", async () => {
    const head = (id: string, older: number) => ({
      ...version(id, 0, true),
      NewestVersion: true,
      OlderVersions: older,
    });
    const page = (items: SecretListItem[]) => ({
      data: { items, total: items.length, limit: 50, offset: 0 },
      allowedMethods: new Set<string>(),
    });

    mockSecrets.mockResolvedValue(page([head("db-v3", 2)]));
    mockSecretVersions.mockResolvedValue([version("db-v2", 0, true), version("db-v1", 0, true)]);
    render(<SecretList />, { wrapper });

    fireEvent.click(await screen.findByRole("button", { name: "+2 older" }));
    await screen.findByText("db-v1");

    mockSecrets.mockResolvedValue(page([head("db-v2", 1)]));
    act(() =>
      broadcast({ type: "secret", action: "remove", id: "db-v3", resource: { ID: "db-v3" } }),
    );

    await waitFor(
      () => expect(mockSecretVersions).toHaveBeenCalledWith("db-v2", expect.anything()),
      { timeout: 2000 },
    );
    expect(screen.getByRole("button", { name: "Hide older" })).toBeInTheDocument();
  });

  // Rotation midway: the old version still runs, so it is listed, not hidden.
  it("marks an older version a service still uses", async () => {
    mockSecrets.mockResolvedValue({
      data: {
        items: [
          { ...version("db-v2", 0, true), NewestVersion: true },
          { ...version("db-v1", 1, true), InUse: true },
        ],
        total: 2,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });
    render(<SecretList />, { wrapper });

    const row = (await screen.findByText("db-v1")).closest("tr");

    expect(row?.textContent).toContain("older, in use");
  });

  it("leaves out a newer head the group fetch saw before the list did", async () => {
    const head = { ...version("db-v2", 1, true), NewestVersion: true, OlderVersions: 1 };

    mockSecrets.mockResolvedValue({
      data: { items: [head], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    mockSecretVersions.mockResolvedValue([
      { ...version("db-v3", 0, true), NewestVersion: true, OlderVersions: 2 },
      head,
      version("db-v1", 0, true),
    ]);
    render(<SecretList />, { wrapper });

    fireEvent.click(await screen.findByRole("button", { name: "+1 older" }));

    expect(await screen.findByText("db-v1")).toBeInTheDocument();
    expect(screen.queryByText("db-v3")).not.toBeInTheDocument();
  });

  it("keeps the toggle outside the card link in grid view", async () => {
    vi.stubGlobal("localStorage", {
      ...localStorageStub,
      getItem: (key: string) => (key === "viewMode:secrets" ? "grid" : null),
    });
    mockSecrets.mockResolvedValue({
      data: {
        items: [{ ...version("db-v2", 1, false), NewestVersion: true, OlderVersions: 1 }],
        total: 1,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });
    render(<SecretList />, { wrapper });

    const toggle = await screen.findByRole("button", { name: "+1 older" });

    expect(toggle.closest("a")).toBeNull();
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
