import AnnotateOverlay from "./AnnotateOverlay";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/hooks/useAnnotationCapabilities", () => ({
  useAnnotationCapabilities: () => ({ create: true, clusterWide: false }),
}));

const service = { type: "service", id: "svc1", name: "web" } as const;

function show() {
  const onDone = vi.fn<() => void>();
  const view = render(
    <QueryClientProvider client={new QueryClient()}>
      <AnnotateOverlay
        timeFor={(x) => 1_000_000 + x * 60}
        scope={service}
        onDone={onDone}
      />
    </QueryClientProvider>,
  );

  return { ...view, overlay: screen.getByTestId("annotate-overlay"), onDone };
}

function click(overlay: HTMLElement, x: number) {
  fireEvent.pointerDown(overlay, { clientX: x, pointerType: "mouse" });
  fireEvent.pointerUp(overlay, { clientX: x, pointerType: "mouse" });
}

describe("AnnotateOverlay", () => {
  it("opens the form at a click, its time filled in", async () => {
    const { overlay } = show();

    click(overlay, 100);

    expect(await screen.findByLabelText("Note")).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "web" })).toBeChecked();
  });

  it("replaces an open mark with a new one", async () => {
    const { overlay } = show();

    click(overlay, 100);
    await screen.findByLabelText("Note");
    click(overlay, 300);

    expect(await screen.findAllByLabelText("Note")).toHaveLength(1);
  });

  it("closes the form when the mode ends", async () => {
    const { overlay, unmount } = show();

    click(overlay, 100);
    await screen.findByLabelText("Note");
    unmount();

    expect(screen.queryByLabelText("Note")).not.toBeInTheDocument();
  });
});
