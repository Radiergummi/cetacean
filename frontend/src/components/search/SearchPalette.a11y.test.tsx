import SearchPalette from "./SearchPalette";
import { api } from "@/api/client";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");

  return { ...actual, api: { ...actual.api, search: vi.fn<typeof actual.api.search>() } };
});

const searchMock = vi.mocked(api.search);

function open() {
  return render(
    <MemoryRouter>
      <SearchPalette onClose={vi.fn<() => void>()} />
    </MemoryRouter>,
  );
}

describe("SearchPalette accessibility", () => {
  beforeEach(() => {
    searchMock.mockReset();
    searchMock.mockResolvedValue({
      results: { services: [{ id: "s1", name: "api", detail: "nginx" }] },
      counts: { services: 1 },
      total: 1,
    } as Awaited<ReturnType<typeof api.search>>);
  });

  it("is a modal dialog wired as a combobox over a listbox", () => {
    open();

    expect(screen.getByRole("dialog")).toBeInTheDocument();

    const input = screen.getByRole("combobox");
    const listbox = screen.getByRole("listbox");

    // The combobox must name the listbox it controls, or a screen reader has
    // no way to reach the results at all.
    expect(input).toHaveAttribute("aria-controls", listbox.id);
    expect(input).toHaveAttribute("aria-autocomplete", "list");
  });

  it("points aria-activedescendant at the highlighted option", async () => {
    const user = userEvent.setup();
    open();

    const input = screen.getByRole("combobox");

    // Typed rather than dispatched: the palette debounces per keystroke, so a
    // single synthetic change event does not exercise the path a person takes.
    await user.type(input, "api");

    const options = await screen.findAllByRole("option", {}, { timeout: 3000 });
    expect(options.length).toBeGreaterThan(0);

    await waitFor(() => {
      const active = input.getAttribute("aria-activedescendant");
      expect(active).toBeTruthy();
      expect(document.getElementById(active!)).toHaveAttribute("aria-selected", "true");
    });
  });

  // Focus management is the half a fireEvent-based test cannot see: user-event
  // moves real focus, so the dialog's trap actually runs.
  it("takes focus on open and does not let Tab fall back to the page", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn<() => void>();
    const trigger = document.createElement("button");
    document.body.append(trigger);
    trigger.focus();

    render(
      <MemoryRouter>
        <SearchPalette onClose={onClose} />
      </MemoryRouter>,
    );

    const dialog = screen.getByRole("dialog");
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));

    // Base UI traps with focus-guard sentinels that sit outside the popup and
    // bounce focus back on a real focus event, which jsdom does not dispatch.
    // What is checkable here is the property that matters: Tab does not land
    // back on the page behind the modal.
    await user.tab();
    expect(document.activeElement).not.toBe(trigger);

    await user.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();

    trigger.remove();
  });
});
