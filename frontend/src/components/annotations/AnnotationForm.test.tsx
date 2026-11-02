import { toLocalInput } from "./annotationFields";
import AnnotationForm from "./AnnotationForm";
import { ApiError } from "@/api/client";
import type { Annotation, AnnotationScope, HistoryEntry, NewAnnotation } from "@/api/types";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const createAnnotation = vi.fn<(body: NewAnnotation) => Promise<HistoryEntry>>();
const updateAnnotation =
  vi.fn<(id: number, patch: Record<string, unknown>, etag?: string) => Promise<Annotation>>();
const readAnnotation =
  vi.fn<(id: number) => Promise<{ data: Annotation; etag?: string | undefined }>>();

vi.mock("@/api/client", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/api/client")>();

  return {
    ...original,
    api: {
      ...original.api,
      createAnnotation: (body: NewAnnotation) => createAnnotation(body),
      updateAnnotation: (id: number, patch: Record<string, unknown>, etag?: string) =>
        updateAnnotation(id, patch, etag),
      annotation: (id: number) => readAnnotation(id),
    },
  };
});

const service = { type: "service", id: "svc1", name: "web" } as const;
const start = Date.UTC(2026, 10, 2, 13, 0) / 1000;

beforeEach(() => {
  createAnnotation.mockReset();
  createAnnotation.mockResolvedValue({} as HistoryEntry);
  updateAnnotation.mockReset();
  updateAnnotation.mockResolvedValue({} as Annotation);
  readAnnotation.mockReset();
});

function show(scopes: Parameters<typeof AnnotationForm>[0]["scopes"] = [service]) {
  const onDone = vi.fn<() => void>();

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

  it("names a stacked resource as the rest of the dashboard does", () => {
    show([{ type: "service", id: "svc2", name: "webshop_frontend" }]);

    expect(screen.getByRole("radio", { name: "webshop/frontend" })).toBeChecked();
  });

  it("keeps the chosen scope when the choices around it change", async () => {
    const user = userEvent.setup();
    const client = new QueryClient();
    const form = (scopes: AnnotationScope[]) => (
      <QueryClientProvider client={client}>
        <AnnotationForm
          scopes={scopes}
          pickResource
          initial={{ from: start }}
          onDone={vi.fn<() => void>()}
        />
      </QueryClientProvider>
    );
    const { rerender } = render(form(["cluster"]));

    await user.click(screen.getByRole("radio", { name: "A service, stack or node" }));
    rerender(form([]));

    expect(screen.getByLabelText("Service, stack or node")).toBeInTheDocument();
  });

  it("adds a typed tag when the field is left", async () => {
    const user = userEvent.setup();

    show();
    await user.type(screen.getByLabelText("Tags"), "deploy");
    await user.click(screen.getByLabelText("Note"));

    expect(screen.getByText("#deploy")).toBeInTheDocument();
    expect(screen.getByLabelText("Tags")).toHaveValue("");
  });
});

describe("AnnotationForm, editing", () => {
  const note: Annotation = {
    id: 7,
    timestamp: "2026-11-02T13:00:30Z",
    until: "2026-11-02T14:00:00Z",
    text: "Load test",
    tags: ["load"],
    resources: [{ type: "service", id: "svc1", name: "webshop_api" }],
  };

  function edit() {
    const onDone = vi.fn<() => void>();

    render(
      <AnnotationForm
        editing={{ note, etag: '"3"' }}
        onDone={onDone}
      />,
    );

    return onDone;
  }

  it("is filled from the note and shows its scope without offering to change it", () => {
    edit();

    expect(screen.getByLabelText("Note")).toHaveValue("Load test");
    expect(screen.getByText("#load")).toBeInTheDocument();
    expect(screen.getByLabelText("Until")).not.toHaveValue("");
    expect(screen.getByText("About").nextElementSibling).toHaveTextContent("webshop/api");
    expect(screen.queryByRole("radio")).not.toBeInTheDocument();
  });

  it("sends only what changed, on the version it was opened at", async () => {
    const user = userEvent.setup();
    const onDone = edit();

    await user.clear(screen.getByLabelText("Note"));
    await user.type(screen.getByLabelText("Note"), "Soak test");
    fireEvent.change(screen.getByLabelText("Until"), { target: { value: "" } });
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(updateAnnotation).toHaveBeenCalledWith(7, { text: "Soak test", until: null }, '"3"');
  });

  it("closes without a request when nothing changed", async () => {
    const user = userEvent.setup();
    const onDone = edit();

    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onDone).toHaveBeenCalled();
    expect(updateAnnotation).not.toHaveBeenCalled();
  });

  it("reloads a note that changed meanwhile and keeps the edits on screen", async () => {
    const user = userEvent.setup();

    updateAnnotation.mockRejectedValueOnce(
      new ApiError("/api/errors/API013", "Precondition Failed", 412, "stale"),
    );
    readAnnotation.mockResolvedValue({
      data: { ...note, text: "Load test, round 2", tags: ["load", "soak"] },
      etag: '"4"',
    });

    const onDone = edit();

    await user.clear(screen.getByLabelText("Note"));
    await user.type(screen.getByLabelText("Note"), "Soak test");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This note changed since you opened it",
    );
    expect(readAnnotation).toHaveBeenCalledWith(7);
    expect(screen.getByLabelText("Note")).toHaveValue("Soak test");
    expect(screen.getByText("#soak")).toBeInTheDocument();
    expect(onDone).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(updateAnnotation).toHaveBeenLastCalledWith(7, { text: "Soak test" }, '"4"');
  });

  it("saves a note whose range is inside one minute without touching its times", async () => {
    const user = userEvent.setup();

    render(
      <AnnotationForm
        editing={{
          note: { ...note, timestamp: "2026-11-02T13:00:10Z", until: "2026-11-02T13:00:40Z" },
          etag: '"3"',
        }}
        onDone={vi.fn<() => void>()}
      />,
    );

    await user.type(screen.getByLabelText("Note"), "!");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateAnnotation).toHaveBeenCalledWith(7, { text: "Load test!" }, '"3"'),
    );
  });

  it("links an error to the field it is about", async () => {
    const user = userEvent.setup();

    edit();
    await user.type(screen.getByLabelText("Tags"), "Bad!{Enter}");

    const tags = screen.getByLabelText("Tags");

    expect(tags).toHaveAttribute("aria-invalid", "true");
    expect(tags.getAttribute("aria-describedby")).toContain(screen.getByRole("alert").id);
  });

  it("checks a moved start against the end's own seconds", async () => {
    const user = userEvent.setup();
    const ranged = {
      ...note,
      timestamp: "2026-11-02T12:00:10Z",
      until: "2026-11-02T12:05:40Z",
    };

    render(
      <AnnotationForm
        editing={{ note: ranged, etag: '"3"' }}
        onDone={vi.fn<() => void>()}
      />,
    );

    const moved = toLocalInput(Date.UTC(2026, 10, 2, 12, 5) / 1000);

    fireEvent.change(screen.getByLabelText("From"), { target: { value: moved } });
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateAnnotation).toHaveBeenCalledWith(
        7,
        { timestamp: "2026-11-02T12:05:00.000Z" },
        '"3"',
      ),
    );
  });
});
