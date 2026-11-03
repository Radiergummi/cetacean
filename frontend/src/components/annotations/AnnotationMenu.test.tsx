import AnnotationMenu from "./AnnotationMenu";
import { ApiError } from "@/api/client";
import type { Annotation } from "@/api/types";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const readAnnotation =
  vi.fn<
    (id: number) => Promise<{ data: Annotation; allowedMethods: Set<string>; etag?: string }>
  >();
const deleteAnnotation = vi.fn<(id: number, etag?: string) => Promise<void>>();

vi.mock("@/api/client", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/api/client")>();

  return {
    ...original,
    api: {
      ...original.api,
      annotation: (id: number) => readAnnotation(id),
      deleteAnnotation: (id: number, etag?: string) => deleteAnnotation(id, etag),
    },
  };
});

const note: Annotation = {
  id: 7,
  timestamp: "2026-11-02T13:00:00Z",
  text: "Load test",
  tags: [],
  resources: [],
};

function allowing(...methods: string[]) {
  readAnnotation.mockResolvedValue({
    data: note,
    allowedMethods: new Set(["GET", "HEAD", ...methods]),
    etag: '"3"',
  });
}

beforeEach(() => {
  readAnnotation.mockReset();
  deleteAnnotation.mockReset();
  deleteAnnotation.mockResolvedValue();
});

describe("AnnotationMenu", () => {
  it("asks about the note only once opened", async () => {
    const user = userEvent.setup();

    allowing("PATCH", "DELETE");
    render(
      <AnnotationMenu
        id={7}
        onActiveChange={() => {}}
      />,
    );

    expect(readAnnotation).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Note actions" }));

    expect(await screen.findByRole("menuitem", { name: "Edit" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
    expect(readAnnotation).toHaveBeenCalledWith(7);
  });

  it("offers only what Allow lists", async () => {
    const user = userEvent.setup();

    allowing("DELETE");
    render(
      <AnnotationMenu
        id={7}
        onActiveChange={() => {}}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Note actions" }));

    expect(await screen.findByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Edit" })).not.toBeInTheDocument();
  });

  it("says so when the note can't be changed", async () => {
    const user = userEvent.setup();

    allowing();
    render(
      <AnnotationMenu
        id={7}
        onActiveChange={() => {}}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Note actions" }));

    expect(
      await screen.findByRole("menuitem", { name: "You can't change this note" }),
    ).toHaveAttribute("aria-disabled", "true");
    expect(screen.queryByRole("menuitem", { name: "Edit" })).not.toBeInTheDocument();
  });

  it("opens the form on the note", async () => {
    const user = userEvent.setup();

    allowing("PATCH");
    render(
      <AnnotationMenu
        id={7}
        onActiveChange={() => {}}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Note actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Edit" }));

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByLabelText("Note")).toHaveValue("Load test");
  });

  it("deletes the version it read, after asking", async () => {
    const user = userEvent.setup();

    allowing("DELETE");
    render(
      <AnnotationMenu
        id={7}
        onActiveChange={() => {}}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Note actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete" }));

    expect(deleteAnnotation).not.toHaveBeenCalled();

    await user.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() => expect(deleteAnnotation).toHaveBeenCalledWith(7, '"3"'));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  });

  it("keeps the confirmation open with the reason when deleting fails", async () => {
    const user = userEvent.setup();

    allowing("DELETE");
    deleteAnnotation.mockRejectedValue(
      new ApiError("/api/errors/API013", "Precondition Failed", 412, "stale"),
    );
    render(
      <AnnotationMenu
        id={7}
        onActiveChange={() => {}}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Note actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete" }));
    await user.click(await screen.findByRole("button", { name: "Delete" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This note changed since you opened it",
    );
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
  });

  it("deletes again when only a removal elsewhere changed the note's validator", async () => {
    const user = userEvent.setup();

    allowing("DELETE");
    deleteAnnotation.mockRejectedValueOnce(
      new ApiError("/api/errors/API013", "Precondition Failed", 412, "stale"),
    );
    render(
      <AnnotationMenu
        id={7}
        onActiveChange={() => {}}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Note actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete" }));
    readAnnotation.mockResolvedValue({
      data: note,
      allowedMethods: new Set(["GET", "HEAD", "DELETE"]),
      etag: '"3-1"',
    });
    await user.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() => expect(deleteAnnotation).toHaveBeenLastCalledWith(7, '"3-1"'));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  });

  it("stays closed when cancelled before a stale delete answers", async () => {
    const user = userEvent.setup();
    let refuse: (failure: unknown) => void = () => {};

    allowing("DELETE");
    deleteAnnotation.mockReturnValue(
      new Promise((_, reject) => {
        refuse = reject;
      }),
    );
    render(
      <AnnotationMenu
        id={7}
        onActiveChange={() => {}}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Note actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete" }));
    await user.click(await screen.findByRole("button", { name: "Delete" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    refuse(new ApiError("/api/errors/API013", "Precondition Failed", 412, "stale"));

    await waitFor(() => expect(readAnnotation).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  });

  it("takes a note already deleted as deleted", async () => {
    const user = userEvent.setup();

    allowing("DELETE");
    deleteAnnotation.mockRejectedValue(
      new ApiError("/api/errors/ANN004", "Not Found", 404, 'annotation "7" not found'),
    );
    render(
      <AnnotationMenu
        id={7}
        onActiveChange={() => {}}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Note actions" }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete" }));
    await user.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() => expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument());
  });
});
