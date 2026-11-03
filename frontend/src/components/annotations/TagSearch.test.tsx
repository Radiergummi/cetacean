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
            { tag: "web-prod", count: 5 },
            { tag: "deploy", count: 4 },
            { tag: "db", count: 2 },
            { tag: "web", count: 1 },
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

  it("leaves a lone # out of the search", async () => {
    const { user, input, params } = show();

    await user.type(input, "web # ");

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

  it("takes a tag typed in full over a longer one used more, and nothing on a bare #", async () => {
    const { user, input, params } = show();

    await user.type(input, "#web");
    await screen.findByRole("option", { name: "#web-prod" });
    await user.keyboard("{Enter}");

    expect(params().getAll("tag")).toEqual(["web"]);

    await user.type(input, "#");
    await screen.findByRole("option", { name: "#web-prod" });
    await user.keyboard("{Enter}");

    expect(params().getAll("tag")).toEqual(["web"]);
  });

  it("types a space before the caret rather than taking the tag at the end", async () => {
    const { user, input, params } = show();

    await user.type(input, "chekout #dep");
    await user.type(input, " ", { initialSelectionStart: 4, initialSelectionEnd: 4 });

    expect(input).toHaveValue("chek out #dep");
    expect(params().getAll("tag")).toEqual([]);
  });

  it("makes a chip of a # word anywhere a space ends it", async () => {
    const { user, input, params } = show();

    await user.click(input);
    await user.paste("#db web");

    expect(params().getAll("tag")).toEqual(["db"]);
    await waitFor(() => expect(params().get("q")).toBe("web"));
  });

  it("offers the tags again on focus after a blur closed them", async () => {
    const { user, input } = show();

    await user.type(input, "#de");
    await screen.findByRole("option", { name: "#deploy" });
    await user.click(document.body);

    expect(screen.queryByRole("option", { name: "#deploy" })).not.toBeInTheDocument();

    await user.click(input);

    expect(screen.getByRole("option", { name: "#deploy" })).toBeInTheDocument();
  });

  it("offers every tag or any tag once two are set", () => {
    show("/history?tag=deploy&tag=db");

    expect(screen.getByRole("button", { name: "Any tag" })).toBeInTheDocument();
  });
});
