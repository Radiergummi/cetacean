import AnnotateOverlay from "./AnnotateOverlay";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/hooks/useClusterAnnotatable", () => ({
  useClusterAnnotatable: () => false,
}));

const service = { type: "service", id: "svc1", name: "web" } as const;

interface Chart {
  timeFor?: ((x: number) => number | null) | undefined;
  locate?: ((time: number) => number | null) | undefined;
  snap?: ((x: number) => number) | undefined;
  capturing?: boolean | undefined;
}

/**
 * The overlay over a chart stand-in whose axis `shift` slides left, as a
 * stream does, saying the markers moved.
 */
function show({
  timeFor = (x) => 1_000_000 + x * 60,
  locate = (time) => (time - 1_000_000) / 60,
  snap,
  capturing = true,
}: Chart = {}) {
  const onDone = vi.fn<() => void>();
  const listeners = new Set<() => void>();
  let shifted = 0;
  const overlay = (capture: boolean) => (
    <QueryClientProvider client={new QueryClient()}>
      <AnnotateOverlay
        timeFor={(x) => timeFor(x + shifted)}
        locate={(time) => {
          const x = locate(time);

          return x === null ? null : x - shifted;
        }}
        subscribe={(listener) => {
          listeners.add(listener);

          return () => {
            listeners.delete(listener);
          };
        }}
        snap={snap}
        scope={service}
        capturing={capture}
        onDone={onDone}
      />
    </QueryClientProvider>
  );
  const view = render(overlay(capturing));

  return {
    ...view,
    overlay: screen.getByTestId("annotate-overlay"),
    onDone,
    release: () => view.rerender(overlay(false)),
    shift: (pixels: number) => {
      shifted += pixels;
      act(() => {
        for (const listener of listeners) {
          listener();
        }
      });
    },
  };
}

function click(overlay: HTMLElement, x: number) {
  fireEvent.pointerDown(overlay, { clientX: x, pointerType: "mouse" });
  fireEvent.mouseDown(overlay, { clientX: x });
  fireEvent.pointerUp(overlay, { clientX: x, pointerType: "mouse" });
  fireEvent.mouseUp(overlay, { clientX: x });
  fireEvent.click(overlay, { clientX: x });
}

describe("AnnotateOverlay", () => {
  it("opens the form at a click, its time filled in", async () => {
    const { overlay } = show();

    click(overlay, 100);

    expect(await screen.findByLabelText("Text")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "web" })).toBeChecked();
  });

  it("follows a drag whose events arrive before a render", async () => {
    const { overlay } = show();

    act(() => {
      fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse" });
      fireEvent.pointerMove(overlay, { clientX: 200, pointerType: "mouse" });
      fireEvent.pointerUp(overlay, { clientX: 300, pointerType: "mouse" });
    });

    expect(await screen.findByLabelText("Text")).toBeInTheDocument();
    expect(screen.getByLabelText("Until")).not.toHaveValue("");
  });

  it("keeps the marked range shaded while the form is open", async () => {
    const { overlay } = show();

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse" });
    fireEvent.pointerUp(overlay, { clientX: 300, pointerType: "mouse" });
    await screen.findByLabelText("Text");

    expect(screen.getByTestId("annotate-mark")).toHaveStyle({ left: "100px", width: "200px" });
  });

  it("draws the mark where its time will be saved", async () => {
    const { overlay } = show({ timeFor: (x) => 1_000_000 + x * 60, snap: (x) => x - 7 });

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse" });
    fireEvent.pointerUp(overlay, { clientX: 300, pointerType: "mouse" });
    await screen.findByLabelText("Text");

    expect(screen.getByTestId("annotate-mark")).toHaveStyle({ left: "93px", width: "200px" });
  });

  it("draws the drag under way where its mark will land", () => {
    const { overlay } = show({ timeFor: (x) => 1_000_000 + x * 60, snap: (x) => x - 7 });

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse", button: 0 });
    fireEvent.pointerMove(overlay, { clientX: 200, pointerType: "mouse" });

    expect(screen.getByTestId("annotate-drag")).toHaveStyle({ left: "93px", width: "100px" });
  });

  it("draws a drag that snaps to one minute as a point", async () => {
    const { overlay } = show({ timeFor: (x) => 1_000_000 + x, snap: () => 50 });

    fireEvent.pointerDown(overlay, { clientX: 40, pointerType: "mouse" });
    fireEvent.pointerUp(overlay, { clientX: 60, pointerType: "mouse" });
    await screen.findByLabelText("Text");

    expect(screen.getByTestId("annotate-mark")).toHaveAttribute("data-point");
  });

  it("finishes a drag whose mode ended before the button came up", async () => {
    const { overlay, release } = show();

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse", button: 0 });
    release();
    fireEvent.pointerUp(overlay, { clientX: 300, pointerType: "mouse", button: 0 });

    expect(await screen.findByLabelText("Text")).toBeInTheDocument();
  });

  it("leaves a secondary button to the context menu", () => {
    const { overlay } = show();

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse", button: 2 });
    fireEvent.pointerUp(overlay, { clientX: 100, pointerType: "mouse", button: 2 });

    expect(screen.queryByLabelText("Text")).not.toBeInTheDocument();
  });

  it("makes a point of a drag shorter than the form's minute", async () => {
    const { overlay } = show({ timeFor: (x) => 1_000_020 + Math.round(x / 10) * 15 });

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse" });
    fireEvent.pointerUp(overlay, { clientX: 110, pointerType: "mouse" });

    expect(await screen.findByLabelText("Until")).toHaveValue("");
  });

  it("replaces an open mark with a new one", async () => {
    const { overlay } = show();

    click(overlay, 100);
    await screen.findByLabelText("Text");
    click(overlay, 300);

    expect(await screen.findAllByLabelText("Text")).toHaveLength(1);
    expect(screen.getByTestId("annotate-mark")).toHaveStyle({ left: "300px" });
  });

  it("closes the form when the mode ends", async () => {
    const { overlay, unmount } = show();

    click(overlay, 100);
    await screen.findByLabelText("Text");
    unmount();

    expect(screen.queryByLabelText("Text")).not.toBeInTheDocument();
  });

  it("leaves the mark where it is while the form is used", async () => {
    const { overlay } = show();

    click(overlay, 100);

    const note = await screen.findByLabelText("Text");

    fireEvent.pointerDown(note, { clientX: 400, pointerType: "mouse" });
    fireEvent.pointerUp(note, { clientX: 400, pointerType: "mouse" });

    expect(screen.getByTestId("annotate-mark")).toHaveStyle({ left: "100px" });
  });

  it("keeps the mark at its time while the chart streams under it", async () => {
    const { overlay, shift } = show();

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse" });
    fireEvent.pointerUp(overlay, { clientX: 300, pointerType: "mouse" });
    await screen.findByLabelText("Text");
    shift(30);

    expect(screen.getByTestId("annotate-mark")).toHaveStyle({ left: "70px", width: "200px" });
  });

  it("starts a drag at the time it was pressed on, though the chart streamed", async () => {
    const { overlay, shift } = show();

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse" });
    shift(30);
    fireEvent.pointerUp(overlay, { clientX: 300, pointerType: "mouse" });
    await screen.findByLabelText("Text");

    expect(screen.getByTestId("annotate-mark")).toHaveStyle({ left: "70px", width: "230px" });
  });

  it("closes an open mark when the next one has no time", async () => {
    let empty = false;
    const { overlay } = show({ timeFor: (x) => (empty ? null : 1_000_000 + x * 60) });

    click(overlay, 100);
    await screen.findByLabelText("Text");
    empty = true;
    click(overlay, 300);

    await waitFor(() => expect(screen.queryByLabelText("Text")).not.toBeInTheDocument());
  });

  it("takes no mark while it does not capture the pointer", () => {
    const { overlay } = show({ capturing: false });

    click(overlay, 100);

    expect(screen.queryByLabelText("Text")).not.toBeInTheDocument();
  });
});
