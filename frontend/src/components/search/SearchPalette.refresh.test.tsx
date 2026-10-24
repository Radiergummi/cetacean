import { ConnectionTracker } from "../ConnectionTracker";
import SearchPalette from "./SearchPalette";
import { api } from "@/api/client";
import { MockEventSource } from "@/test/mocks";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");

  return { ...actual, api: { ...actual.api, search: vi.fn<typeof actual.api.search>() } };
});

const searchMock = vi.mocked(api.search);

function response(detail: string) {
  return {
    results: { services: [{ id: "s1", name: "api", detail }] },
    counts: { services: 1 },
    total: 1,
  } as Awaited<ReturnType<typeof api.search>>;
}

describe("SearchPalette refresh", () => {
  beforeEach(() => {
    MockEventSource.instances = [];
    vi.stubGlobal("EventSource", MockEventSource);
    searchMock.mockReset();
    searchMock.mockResolvedValue(response("nginx:1"));
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("refreshes shown results after cluster events, not on a timer", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) });

    render(
      <MemoryRouter>
        <ConnectionTracker>
          <SearchPalette onClose={vi.fn<() => void>()} />
        </ConnectionTracker>
      </MemoryRouter>,
    );

    await user.type(screen.getByRole("combobox"), "api");
    await act(() => vi.advanceTimersByTimeAsync(300));
    expect(await screen.findByText("nginx:1")).toBeInTheDocument();

    const searches = searchMock.mock.calls.length;
    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(searchMock.mock.calls.length).toBe(searches);

    // The palette rides the tracker's stream rather than opening its own.
    expect(MockEventSource.instances).toHaveLength(1);

    searchMock.mockResolvedValue(response("nginx:2"));
    act(() => {
      for (const id of ["a", "b", "c"]) {
        MockEventSource.instance.simulateEvent("task", { type: "task", action: "update", id });
      }
    });
    await act(() => vi.advanceTimersByTimeAsync(600));

    expect(searchMock.mock.calls.length).toBe(searches + 1);
    expect(screen.getByText("nginx:2")).toBeInTheDocument();
  });

  async function openWithResults() {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) });

    render(
      <MemoryRouter>
        <ConnectionTracker>
          <SearchPalette onClose={vi.fn<() => void>()} />
        </ConnectionTracker>
      </MemoryRouter>,
    );

    const input = screen.getByRole("combobox");
    await user.type(input, "api");
    await act(() => vi.advanceTimersByTimeAsync(300));
    await screen.findByText("nginx:1");

    return { user, input };
  }

  function emit(id: string) {
    act(() => {
      MockEventSource.instance.simulateEvent("task", { type: "task", action: "update", id });
    });
  }

  it("keeps refreshing through a steady stream of events, no faster than every 2s", async () => {
    await openWithResults();
    const searches = searchMock.mock.calls.length;

    for (let tick = 0; tick < 20; tick++) {
      emit(`t${tick}`);
      await act(() => vi.advanceTimersByTimeAsync(300));
    }

    const refreshes = searchMock.mock.calls.length - searches;
    expect(refreshes).toBeGreaterThanOrEqual(2);
    expect(refreshes).toBeLessThanOrEqual(4);
  });

  it("drops a queued refresh when the query is cleared", async () => {
    const { user, input } = await openWithResults();
    const searches = searchMock.mock.calls.length;

    emit("a");
    await user.clear(input);
    await act(() => vi.advanceTimersByTimeAsync(3_000));

    expect(searchMock.mock.calls.slice(searches).map(([query]) => query)).not.toContain("");
    expect(searchMock.mock.calls.length).toBe(searches);
  });

  it("refreshes the query its results answer, not one still being typed", async () => {
    const { user, input } = await openWithResults();
    const searches = searchMock.mock.calls.length;

    // The search for what is typed now is still in flight when the refresh runs.
    searchMock.mockImplementation((query) =>
      query === "api" ? Promise.resolve(response("nginx:2")) : new Promise(() => {}),
    );

    emit("a");
    await user.type(input, "x");
    await act(() => vi.advanceTimersByTimeAsync(600));

    expect(searchMock.mock.calls.slice(searches).map(([query]) => query)).toEqual(["apix", "api"]);
    expect(screen.getByText("nginx:2")).toBeInTheDocument();
  });
});
