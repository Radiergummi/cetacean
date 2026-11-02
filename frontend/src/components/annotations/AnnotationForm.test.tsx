import AnnotationForm from "./AnnotationForm";
import { ApiError } from "@/api/client";
import type { HistoryEntry, NewAnnotation } from "@/api/types";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const createAnnotation = vi.fn<(body: NewAnnotation) => Promise<HistoryEntry>>();

vi.mock("@/api/client", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/api/client")>();

  return {
    ...original,
    api: {
      ...original.api,
      createAnnotation: (body: NewAnnotation) => createAnnotation(body),
    },
  };
});

const service = { type: "service", id: "svc1", name: "web" } as const;
const start = Date.UTC(2026, 10, 2, 13, 0) / 1000;

beforeEach(() => {
  createAnnotation.mockReset();
  createAnnotation.mockResolvedValue({} as HistoryEntry);
});

function show(scopes: Parameters<typeof AnnotationForm>[0]["scopes"] = [service]) {
  const onDone = vi.fn();

  render(
    <AnnotationForm
      scopes={scopes}
      initial={{ from: start }}
      onDone={onDone}
    />,
  );

  return onDone;
}

describe("AnnotationForm", () => {
  it("creates a note on the page's resource", async () => {
    const user = userEvent.setup();
    const onDone = show();

    await user.type(screen.getByLabelText("Note"), "Deployed the fix");
    await user.type(screen.getByLabelText("Tags"), "deploy{Enter}rollback,");
    await user.click(screen.getByRole("button", { name: "Add annotation" }));

    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(createAnnotation).toHaveBeenCalledTimes(1);
    expect(createAnnotation.mock.calls[0]?.[0]).toMatchObject({
      text: "Deployed the fix",
      tags: ["deploy", "rollback"],
      resources: [{ type: "service", id: "svc1" }],
    });
  });

  it("refuses an invalid tag inline", async () => {
    const user = userEvent.setup();

    show();
    await user.type(screen.getByLabelText("Tags"), "Bad!{Enter}");

    expect(screen.getByRole("alert")).toHaveTextContent(/tag/i);
    expect(screen.queryByText("#bad!")).not.toBeInTheDocument();
  });

  it("requires text and an end after the start", async () => {
    const user = userEvent.setup();

    show();

    expect(screen.getByRole("button", { name: "Add annotation" })).toBeDisabled();

    await user.type(screen.getByLabelText("Note"), "x");
    fireEvent.change(screen.getByLabelText("Until"), { target: { value: "2000-01-01T00:00" } });
    await user.click(screen.getByRole("button", { name: "Add annotation" }));

    expect(screen.getByRole("alert")).toHaveTextContent(/after/i);
    expect(createAnnotation).not.toHaveBeenCalled();
  });

  it("shows the server's refusal and keeps the input", async () => {
    const user = userEvent.setup();

    createAnnotation.mockRejectedValue(
      new ApiError("/api/errors/ANN001", "Forbidden", 403, "You may not annotate service web"),
    );

    const onDone = show();

    await user.type(screen.getByLabelText("Note"), "Deployed the fix");
    await user.click(screen.getByRole("button", { name: "Add annotation" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("You may not annotate service web");
    expect(screen.getByLabelText("Note")).toHaveValue("Deployed the fix");
    expect(onDone).not.toHaveBeenCalled();
  });

  it("sends one request however often it is submitted", async () => {
    const user = userEvent.setup();

    createAnnotation.mockReturnValue(new Promise(() => {}));
    show();

    await user.type(screen.getByLabelText("Note"), "x");

    const form = screen.getByRole("button", { name: "Add annotation" }).closest("form")!;

    fireEvent.submit(form);
    fireEvent.submit(form);

    expect(createAnnotation).toHaveBeenCalledTimes(1);
  });

  it("offers cluster-wide when allowed", async () => {
    const user = userEvent.setup();

    show([service, "cluster"]);

    expect(screen.getByRole("radio", { name: "web" })).toBeChecked();

    await user.click(screen.getByRole("radio", { name: "Cluster-wide" }));
    await user.type(screen.getByLabelText("Note"), "x");
    await user.click(screen.getByRole("button", { name: "Add annotation" }));

    await waitFor(() => expect(createAnnotation).toHaveBeenCalled());
    expect(createAnnotation.mock.calls[0]?.[0]).not.toHaveProperty("resources");
  });
});
