import TagSearch from "./TagSearch";
import { createTestQueryClient } from "@/test/mocks";
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/api/client")>();

  return {
    ...original,
    api: {
      ...original.api,
      annotationTagsPage: (path: string) =>
        Promise.resolve({
          items: [
            { tag: "deploy", count: 4 },
            { tag: "db", count: 2 },
          ].filter(({ tag }) =>
            tag.startsWith(new URL(path, "http://x").searchParams.get("q") ?? ""),
          ),
        }),
    },
  };
});

function Location() {
  const { search } = useLocation();

  return <output data-testid="location">{search}</output>;
}

function show(url = "/history") {
  render(
    <QueryClientProvider client={createTestQueryClient()}>
      <MemoryRouter initialEntries={[url]}>
        <TagSearch placeholder="Search" />
        <Location />
      </MemoryRouter>
    </QueryClientProvider>,
  );

  const params = () => new URLSearchParams(screen.getByTestId("location").textContent ?? "");

  return { user: userEvent.setup(), input: screen.getByRole("combobox"), params };
}

describe("TagSearch", () => {
  it("completes a # word from the tags in use and keeps it as a chip on Tab", async () => {
    const { user, input, params } = show();

    await user.type(input, "#dep");

    expect(await screen.findByRole("option", { name: "#deploy" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "#db" })).not.toBeInTheDocument();

    await user.keyboard("{Tab}");

    expect(params().getAll("tag")).toEqual(["deploy"]);
    expect(input).toHaveValue("");
    expect(input).toHaveFocus();
    expect(screen.getByRole("button", { name: "Remove the deploy filter" })).toBeInTheDocument();
  });

  it("keeps a new tag as typed on Space, and searches the other words", async () => {
    const { user, input, params } = show();

    await user.type(input, "web #rollback ");

    expect(params().getAll("tag")).toEqual(["rollback"]);
    expect(input).toHaveValue("web ");
    await waitFor(() => expect(params().get("q")).toBe("web"));
  });

  it("takes a chip back on Backspace in an empty box, and refuses an invalid tag", async () => {
    const { user, input, params } = show("/history?tag=deploy&tag=db");

    await user.click(input);
    await user.keyboard("{Backspace}");

    expect(params().getAll("tag")).toEqual(["deploy"]);

    await user.type(input, "#bad!{Enter}");

    expect(screen.getByRole("alert")).toHaveTextContent(/lowercase letters/);
    expect(params().getAll("tag")).toEqual(["deploy"]);
  });

  it("offers every tag or any tag once two are set", () => {
    show("/history?tag=deploy&tag=db");

    expect(screen.getByRole("button", { name: "Any tag" })).toBeInTheDocument();
  });
});
