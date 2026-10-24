import SearchPalette from "./SearchPalette";
import { ApiError, api, headCapabilities } from "@/api/client";
import { MockEventSource } from "@/test/mocks";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");

  return {
    ...actual,
    headCapabilities: vi.fn<typeof actual.headCapabilities>(),
    api: {
      ...actual.api,
      search: vi.fn<typeof actual.api.search>(),
      removeNode: vi.fn<typeof actual.api.removeNode>(),
    },
  };
});

const removeNodeMock = vi.mocked(api.removeNode);

const notDown = new ApiError("/api/errors/NOD001", "Node not down", 409, "node is not down");

async function pickNodeToRemove() {
  const user = userEvent.setup();

  render(
    <MemoryRouter>
      <SearchPalette onClose={vi.fn<() => void>()} />
    </MemoryRouter>,
  );

  const input = screen.getByRole("combobox");
  await user.type(input, "remove node");
  await user.keyboard("{Enter}");
  await user.type(input, "worker");
  await user.click(await screen.findByRole("option", { name: /worker-1/ }));
  await user.click(await screen.findByRole("button", { name: "Confirm" }));

  return user;
}

describe("SearchPalette Remove Node", () => {
  beforeEach(() => {
    vi.stubGlobal("EventSource", MockEventSource);
    vi.mocked(headCapabilities).mockResolvedValue({
      methods: new Set(["GET", "DELETE"]),
      editable: new Set(),
    });
    vi.mocked(api.search).mockResolvedValue({
      results: { nodes: [{ id: "n1", name: "worker-1", detail: "worker" }] },
      counts: { nodes: 1 },
      total: 1,
    } as Awaited<ReturnType<typeof api.search>>);
    removeNodeMock.mockReset();
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("tries a normal removal first", async () => {
    removeNodeMock.mockResolvedValue(undefined as never);

    await pickNodeToRemove();

    await waitFor(() => expect(removeNodeMock).toHaveBeenCalledTimes(1));
    expect(removeNodeMock.mock.calls[0]).toEqual(["n1"]);
  });

  it("offers force only after Docker refuses, and says so", async () => {
    removeNodeMock.mockRejectedValueOnce(notDown).mockResolvedValueOnce(undefined as never);

    const user = await pickNodeToRemove();

    expect(await screen.findByText(/Force Remove Node/)).toBeInTheDocument();
    expect(screen.getByText(/refused the normal remove/i)).toBeInTheDocument();
    expect(removeNodeMock).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => expect(removeNodeMock).toHaveBeenCalledTimes(2));
    expect(removeNodeMock.mock.calls[1]).toEqual(["n1", true]);
  });

  it("reports a permission check that fails instead of swallowing it", async () => {
    vi.mocked(headCapabilities).mockRejectedValue(new Error("network down"));
    const user = userEvent.setup();

    render(
      <MemoryRouter>
        <SearchPalette onClose={vi.fn<() => void>()} />
      </MemoryRouter>,
    );

    const input = screen.getByRole("combobox");
    await user.type(input, "remove node");
    await user.keyboard("{Enter}");
    await user.type(input, "worker");
    await user.click(await screen.findByRole("option", { name: /worker-1/ }));

    expect(await screen.findByText(/network down/)).toBeInTheDocument();
    expect(removeNodeMock).not.toHaveBeenCalled();
  });

  it("does not offer force for a refusal force would not get past", async () => {
    removeNodeMock.mockRejectedValue(
      new ApiError("/api/errors/NOD003", "Node not found", 404, "no such node"),
    );

    await pickNodeToRemove();

    expect(await screen.findByText(/no such node/)).toBeInTheDocument();
    expect(screen.queryByText(/Force Remove Node/)).not.toBeInTheDocument();
    expect(removeNodeMock).toHaveBeenCalledTimes(1);
  });
});
