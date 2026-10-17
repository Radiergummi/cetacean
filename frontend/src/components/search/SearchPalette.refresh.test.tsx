import { ConnectionTracker } from "../ConnectionTracker";
import SearchPalette from "./SearchPalette";
import { api } from "@/api/client";
import { MockEventSource } from "@/test/mocks";
import { act, render, screen } from "@testing-library/react";
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
});
