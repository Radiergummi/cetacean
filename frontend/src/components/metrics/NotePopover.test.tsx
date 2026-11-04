import NotePopover from "./NotePopover";
import type { TimelineItem } from "@/lib/timeline";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { render, screen } from "@testing-library/react";
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

function show(ids: number[], timeline: TimelineItem[], onClose = vi.fn<() => void>()) {
  const container = { current: document.body };

  const view = render(
    <NotePopover
      pin={{ x: 10, y: 0, ids }}
      container={container}
      timeline={timeline}
      onClose={onClose}
    />,
    { wrapper: createWrapper(createTestQueryClient()) },
  );

  return { ...view, onClose, container };
}

describe("NotePopover", () => {
  it("lists the flag's notes in its order, each with its menu and a link to its page", async () => {
    show([2, 1], [note(1, "Older"), note(2, "Newer")]);

    const cards = await screen.findAllByText(/Older|Newer/);

    expect(cards.map(({ textContent }) => textContent)).toEqual(["Newer", "Older"]);
    expect(screen.getAllByRole("button", { name: "Note actions" })).toHaveLength(2);
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
    const { rerender, container, onClose } = show([1, 2], [note(1, "Kept"), note(2, "Deleted")]);

    await screen.findByText("Deleted");
    rerender(
      <NotePopover
        pin={{ x: 10, y: 0, ids: [1, 2] }}
        container={container}
        timeline={[note(1, "Kept")]}
        onClose={onClose}
      />,
    );

    expect(screen.queryByText("Deleted")).not.toBeInTheDocument();
    expect(screen.getByText("Kept")).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();

    rerender(
      <NotePopover
        pin={{ x: 10, y: 0, ids: [1, 2] }}
        container={container}
        timeline={[]}
        onClose={onClose}
      />,
    );

    expect(onClose).toHaveBeenCalled();
  });
});
