import { ApiError, type Page } from "../api/client";
import type { Annotation, AnnotationTag } from "../api/types";
import { AuthContext } from "../hooks/useAuth";
import { MockEventSource, createTestQueryClient } from "../test/mocks";
import AnnotationDetail from "./AnnotationDetail";
import Annotations from "./Annotations";
import AnnotationTags from "./AnnotationTags";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../api/client", async (importOriginal) => {
  const original = await importOriginal<typeof import("../api/client")>();

  return {
    ...original,
    api: {
      annotationsPath: original.api.annotationsPath,
      annotationsPage: vi.fn<() => Promise<Page<Annotation>>>(),
      annotationTagsPage: vi.fn<() => Promise<Page<AnnotationTag>>>(),
      annotation: vi.fn<typeof original.api.annotation>(),
    },
  };
});

vi.mock("../hooks/useClusterAnnotatable", () => ({
  useClusterAnnotatable: () => false,
}));

import { api } from "../api/client";
const annotationsPage = vi.mocked(api.annotationsPage);
const annotationTagsPage = vi.mocked(api.annotationTagsPage);
const readAnnotation = vi.mocked(api.annotation);

const note: Annotation = {
  id: 7,
  timestamp: "2026-11-02T13:00:00Z",
  text: "Load test",
  tags: ["load"],
  resources: [
    { type: "service", id: "svc1", name: "web" },
    { type: "node", id: "node1", name: "worker", removed: true },
  ],
  actor: { subject: "alice", name: "Alice", provider: "oidc", transport: "dashboard" },
};

let queryClient: QueryClient;

beforeEach(() => {
  queryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  annotationsPage.mockReset();
  annotationTagsPage.mockReset();
  readAnnotation.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function renderAt(path: string, page: ReactNode) {
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext
        value={{
          identity: { subject: "alice", displayName: "Alice", provider: "oidc" },
          loading: false,
        }}
      >
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route
              path="/annotations"
              element={page}
            />
            <Route
              path="/annotations/tags"
              element={page}
            />
            <Route
              path="/annotations/:id"
              element={page}
            />
          </Routes>
        </MemoryRouter>
      </AuthContext>
    </QueryClientProvider>,
  );
}

describe("Annotations", () => {
  it("asks for the URL's filters and shows the notes", async () => {
    annotationsPage.mockResolvedValue({ items: [note] });
    renderAt("/annotations?tag=load&tag=web&matchAny=true", <Annotations />);

    expect(await screen.findByText("Load test")).toBeInTheDocument();
    expect(annotationsPage).toHaveBeenCalledWith(
      "/annotations?tag=load&tag=web&matchAny=true&limit=200",
      expect.anything(),
    );
    expect(screen.getByRole("button", { name: "Any tag" })).toHaveAttribute("aria-pressed", "true");
  });

  it("narrows to the caller's own notes", async () => {
    const user = userEvent.setup();

    annotationsPage.mockResolvedValue({ items: [] });
    renderAt("/annotations", <Annotations />);

    await user.click(await screen.findByRole("button", { name: "Me" }));

    await waitFor(() =>
      expect(annotationsPage).toHaveBeenLastCalledWith(
        "/annotations?author=alice&limit=200",
        expect.anything(),
      ),
    );
  });

  it("drops a tag filter", async () => {
    const user = userEvent.setup();

    annotationsPage.mockResolvedValue({ items: [] });
    renderAt("/annotations?tag=load", <Annotations />);

    await user.click(await screen.findByRole("button", { name: "Remove the load filter" }));

    await waitFor(() =>
      expect(annotationsPage).toHaveBeenLastCalledWith("/annotations?limit=200", expect.anything()),
    );
  });

  it("links a note's tags to the notes carrying them", async () => {
    annotationsPage.mockResolvedValue({ items: [note] });
    renderAt("/annotations", <Annotations />);

    expect(await screen.findByRole("link", { name: "#load" })).toHaveAttribute(
      "href",
      "/annotations?tag=load",
    );
    expect(screen.getByRole("link", { name: /^about / })).toHaveAttribute("href", "/annotations/7");
  });
});

describe("AnnotationTags", () => {
  it("lists the tags, each linking its notes", async () => {
    annotationTagsPage.mockResolvedValue({
      items: [
        { tag: "deploy", count: 3 },
        { tag: "load", count: 1 },
      ],
    });
    renderAt("/annotations/tags", <AnnotationTags />);

    expect(await screen.findByRole("link", { name: /#deploy\s*3 notes/ })).toHaveAttribute(
      "href",
      "/annotations?tag=deploy",
    );
    expect(screen.getByText("1 note")).toBeInTheDocument();
  });
});

describe("AnnotationDetail", () => {
  it("shows the note, linking only the resources still there", async () => {
    readAnnotation.mockResolvedValue({
      data: note,
      allowedMethods: new Set(["GET", "HEAD"]),
      etag: '"7-2"',
    });
    renderAt("/annotations/7", <AnnotationDetail />);

    expect(await screen.findByText("Load test")).toBeInTheDocument();
    expect(readAnnotation).toHaveBeenCalledWith(7, expect.anything());
    expect(screen.getAllByRole("link", { name: "web" })[0]).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "worker" })).not.toBeInTheDocument();
    expect(screen.getByText("(removed)")).toBeInTheDocument();
  });

  it("says when the note is not there", async () => {
    readAnnotation.mockRejectedValue(
      new ApiError("/api/errors/ANN004", "Not Found", 404, 'annotation "7" not found'),
    );
    renderAt("/annotations/7", <AnnotationDetail />);

    expect(await screen.findByText(/doesn't exist, or you may not read it/)).toBeInTheDocument();
  });
});
