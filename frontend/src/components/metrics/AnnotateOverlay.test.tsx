import AnnotateOverlay from "./AnnotateOverlay";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/hooks/useAnnotationCapabilities", () => ({
  useAnnotationCapabilities: () => ({ create: true, clusterWide: false }),
}));

const service = { type: "service", id: "svc1", name: "web" } as const;

function show(timeFor: (x: number) => number = (x) => 1_000_000 + x * 60, capturing = true) {
  const onDone = vi.fn<() => void>();
  const view = render(
    <QueryClientProvider client={new QueryClient()}>
      <AnnotateOverlay
        timeFor={timeFor}
        scope={service}
        capturing={capturing}
        onDone={onDone}
      />
    </QueryClientProvider>,
  );

  return { ...view, overlay: screen.getByTestId("annotate-overlay"), onDone };
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

    expect(await screen.findByLabelText("Note")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "web" })).toBeChecked();
  });

  it("follows a drag whose events arrive before a render", async () => {
    const { overlay } = show();

    act(() => {
      fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse" });
      fireEvent.pointerMove(overlay, { clientX: 200, pointerType: "mouse" });
      fireEvent.pointerUp(overlay, { clientX: 300, pointerType: "mouse" });
    });

    expect(await screen.findByLabelText("Note")).toBeInTheDocument();
    expect(screen.getByLabelText("Until")).not.toHaveValue("");
  });

  it("keeps the marked range shaded while the form is open", async () => {
    const { overlay } = show();

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse" });
    fireEvent.pointerUp(overlay, { clientX: 300, pointerType: "mouse" });
    await screen.findByLabelText("Note");

    expect(screen.getByTestId("annotate-mark")).toHaveStyle({ left: "100px", width: "200px" });
  });

  it("makes a point of a drag shorter than the form's minute", async () => {
    const { overlay } = show((x) => 1_000_020 + Math.round(x / 10) * 15);

    fireEvent.pointerDown(overlay, { clientX: 100, pointerType: "mouse" });
    fireEvent.pointerUp(overlay, { clientX: 110, pointerType: "mouse" });

    expect(await screen.findByLabelText("Until")).toHaveValue("");
  });

  it("replaces an open mark with a new one", async () => {
    const { overlay } = show();

    click(overlay, 100);
    await screen.findByLabelText("Note");
    click(overlay, 300);

    expect(await screen.findAllByLabelText("Note")).toHaveLength(1);
    expect(screen.getByTestId("annotate-mark")).toHaveStyle({ left: "300px" });
  });

  it("closes the form when the mode ends", async () => {
    const { overlay, unmount } = show();

    click(overlay, 100);
    await screen.findByLabelText("Note");
    unmount();

    expect(screen.queryByLabelText("Note")).not.toBeInTheDocument();
  });

  it("leaves the mark where it is while the form is used", async () => {
    const { overlay } = show();

    click(overlay, 100);

    const note = await screen.findByLabelText("Note");

    fireEvent.pointerDown(note, { clientX: 400, pointerType: "mouse" });
    fireEvent.pointerUp(note, { clientX: 400, pointerType: "mouse" });

    expect(screen.getByTestId("annotate-mark")).toHaveStyle({ left: "100px" });
  });

  it("takes no mark while it does not capture the pointer", () => {
    const { overlay } = show(undefined, false);

    click(overlay, 100);

    expect(screen.queryByLabelText("Note")).not.toBeInTheDocument();
  });
});
