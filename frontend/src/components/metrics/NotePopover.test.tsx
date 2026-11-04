import NotePopover from "./NotePopover";
import type { TimelineItem } from "@/lib/timeline";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const note = (id: number, text: string): TimelineItem => ({
  shape: "point",
  kind: "annotation",
  at: 1_000 + id,
  entry: {
    id,
    kind: "annotation",
    timestamp: "2026-11-03T12:00:00Z",
    type: "",
    action: "",
    resourceId: "",
    name: "",
    text,
    tags: [],
  },
});

/**
 * A chart stand-in: where the pinned time sits, and a way to say it moved.
 */
function chart(at: number | null = 10) {
  const listeners = new Set<() => void>();
  const locate = vi.fn<(time: number) => number | null>(() => at);

  return {
    locate,
    subscribe: (listener: () => void) => {
      listeners.add(listener);

      return () => {
        listeners.delete(listener);
      };
    },
    move: (next: number | null) => {
      at = next;
      act(() => {
        for (const listener of listeners) {
          listener();
        }
      });
    },
  };
}

function show(ids: number[], timeline: TimelineItem[], onClose = vi.fn<() => void>()) {
  const container = { current: document.body };
  const plot = chart();
  const popover = (items: TimelineItem[]) => (
    <NotePopover
      pin={{ time: 1_000, y: 0, ids }}
      container={container}
      timeline={items}
      locate={plot.locate}
      subscribe={plot.subscribe}
      onClose={onClose}
    />
  );

  const view = render(popover(timeline), { wrapper: createWrapper(createTestQueryClient()) });

  return {
    ...view,
    onClose,
    plot,
    rerenderWith: (items: TimelineItem[]) => view.rerender(popover(items)),
  };
}

describe("NotePopover", () => {
  it("lists the flag's notes in its order, each with its menu and a link to its page", async () => {
    show([2, 1], [note(1, "Older"), note(2, "Newer")]);

    const cards = await screen.findAllByText(/Older|Newer/);

    expect(cards.map(({ textContent }) => textContent)).toEqual(["Newer", "Older"]);
    expect(screen.getAllByRole("button", { name: "Annotation actions" })).toHaveLength(2);
    expect(screen.getAllByRole("link", { name: "Open" })[0]).toHaveAttribute(
      "href",
      "/annotations/2",
    );
  });

  it("closes on Escape", async () => {
    const { onClose } = show([1], [note(1, "Only")]);

    await screen.findByText("Only");
    await userEvent.keyboard("{Escape}");

    expect(onClose).toHaveBeenCalled();
  });

  it("drops a note the timeline no longer holds, and unpins once none is left", async () => {
    const { rerenderWith, onClose } = show([1, 2], [note(1, "Kept"), note(2, "Deleted")]);

    await screen.findByText("Deleted");
    rerenderWith([note(1, "Kept")]);

    expect(screen.queryByText("Deleted")).not.toBeInTheDocument();
    expect(screen.getByText("Kept")).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();

    rerenderWith([]);

    expect(onClose).toHaveBeenCalled();
  });

  it("places itself at its time again whenever the chart's markers move", async () => {
    const { plot, onClose } = show([1], [note(1, "Moving")]);

    const placement = () => screen.getByRole("dialog").parentElement?.style.transform;

    await screen.findByText("Moving");
    await waitFor(() => expect(placement()).toMatch(/translate/));

    const before = placement();
    plot.locate.mockClear();
    plot.move(300);

    expect(plot.locate).toHaveBeenCalledWith(1_000);
    await waitFor(() => expect(placement()).not.toBe(before));
    expect(onClose).not.toHaveBeenCalled();
  });

  it("unpins once its time has scrolled off the chart", async () => {
    const { plot, onClose } = show([1], [note(1, "Leaving")]);

    await screen.findByText("Leaving");
    plot.move(null);

    expect(onClose).toHaveBeenCalled();
  });
});
