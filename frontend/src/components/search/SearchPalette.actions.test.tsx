import SearchPalette from "./SearchPalette";
import { api, headCapabilities } from "@/api/client";
import type { Plugin } from "@/api/types";
import { MockEventSource } from "@/test/mocks";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async () => {
  const actual = await vi.importActual<typeof import("@/api/client")>("@/api/client");

  return {
    ...actual,
    headCapabilities: vi.fn<typeof actual.headCapabilities>(),
    api: {
      ...actual.api,
      search: vi.fn<typeof actual.api.search>(),
      rotateToken: vi.fn<typeof actual.api.rotateToken>(),
      plugins: vi.fn<typeof actual.api.plugins>(),
      disablePlugin: vi.fn<typeof actual.api.disablePlugin>(),
      annotationsPage: vi.fn<typeof actual.api.annotationsPage>(),
    },
  };
});

function open(onClose = vi.fn<() => void>()) {
  render(
    <MemoryRouter>
      <SearchPalette onClose={onClose} />
    </MemoryRouter>,
  );

  return { user: userEvent.setup(), input: screen.getByRole("combobox"), onClose };
}

describe("SearchPalette actions without a resource step", () => {
  beforeEach(() => {
    vi.stubGlobal("EventSource", MockEventSource);
    vi.mocked(api.search).mockResolvedValue({ results: {}, counts: {}, total: 0 } as Awaited<
      ReturnType<typeof api.search>
    >);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  it("checks the swarm's edit link and confirms before rotating a token", async () => {
    vi.mocked(headCapabilities).mockResolvedValue({
      methods: new Set(["GET"]),
      editable: new Set(["rotate-token"]),
    });
    vi.mocked(api.rotateToken).mockResolvedValue(undefined as never);

    const { user, input } = open();

    await user.type(input, "rotate worker token");
    await user.keyboard("{Enter}");

    expect(await screen.findByText(/Confirm: Rotate Worker Join Token/)).toBeInTheDocument();
    expect(headCapabilities).toHaveBeenCalledWith("/swarm");
    expect(api.rotateToken).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => expect(api.rotateToken).toHaveBeenCalledWith("worker"));
  });

  it("refuses a rotation the swarm doesn't link as editable", async () => {
    vi.mocked(headCapabilities).mockResolvedValue({
      methods: new Set(["GET"]),
      editable: new Set(),
    });

    const { user, input } = open();

    await user.type(input, "rotate worker token");
    await user.keyboard("{Enter}");

    expect(await screen.findByText(/don't have permission/)).toBeInTheDocument();
    expect(api.rotateToken).not.toHaveBeenCalled();
  });

  it("lists the enabled plugins to disable one, checked on that plugin", async () => {
    vi.mocked(api.plugins).mockResolvedValue({
      data: [
        { Name: "vieux/sshfs:latest", Enabled: true },
        { Name: "rexray/ebs:latest", Enabled: false },
      ] as Plugin[],
      allowedMethods: new Set(["GET"]),
    });
    vi.mocked(headCapabilities).mockResolvedValue({
      methods: new Set(["GET"]),
      editable: new Set(["disable"]),
    });
    vi.mocked(api.disablePlugin).mockResolvedValue(undefined as never);

    const { user, input } = open();

    await user.type(input, "disable plugin");
    await user.keyboard("{Enter}");

    expect(await screen.findByRole("option", { name: "vieux/sshfs:latest" })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: "rexray/ebs:latest" })).not.toBeInTheDocument();

    await user.type(input, "sshfs");
    await user.keyboard("{Enter}");
    await user.click(await screen.findByRole("button", { name: "Confirm" }));

    expect(headCapabilities).toHaveBeenCalledWith("/plugins/vieux%2Fsshfs%3Alatest");
    await waitFor(() => expect(api.disablePlugin).toHaveBeenCalledWith("vieux/sshfs:latest"));
  });

  it("acts on the plugin picked after a cancelled confirmation, not the first", async () => {
    vi.mocked(api.plugins).mockResolvedValue({
      data: [
        { Name: "a/one:latest", Enabled: true },
        { Name: "b/two:latest", Enabled: true },
      ] as Plugin[],
      allowedMethods: new Set(["GET"]),
    });
    vi.mocked(headCapabilities).mockResolvedValue({
      methods: new Set(["GET"]),
      editable: new Set(["disable"]),
    });
    vi.mocked(api.disablePlugin).mockResolvedValue(undefined as never);

    const { user, input } = open();

    await user.type(input, "disable plugin");
    await user.keyboard("{Enter}");
    await user.click(await screen.findByRole("option", { name: "a/one:latest" }));
    await user.click(await screen.findByRole("button", { name: "Cancel" }));
    await user.click(screen.getByRole("option", { name: "b/two:latest" }));
    await user.click(await screen.findByRole("button", { name: "Confirm" }));

    await waitFor(() => expect(api.disablePlugin).toHaveBeenCalledWith("b/two:latest"));
    expect(api.disablePlugin).not.toHaveBeenCalledWith("a/one:latest");
  });

  it("goes back to searching after a step-less action is cancelled", async () => {
    vi.mocked(headCapabilities).mockResolvedValue({
      methods: new Set(["GET"]),
      editable: new Set(["rotate-token"]),
    });

    const { user, input } = open();

    await user.type(input, "rotate worker token");
    await user.keyboard("{Enter}");
    await user.click(await screen.findByRole("button", { name: "Cancel" }));

    expect(input).toHaveAttribute("placeholder", "Search resources…");

    await user.type(input, "web");

    await waitFor(() =>
      expect(api.search).toHaveBeenCalledWith("web", undefined, expect.anything()),
    );
  });

  it("opens the annotation form where the caller may annotate, and says so where not", async () => {
    const opened = vi.fn<() => void>();

    window.addEventListener("cetacean:add-annotation", opened);
    vi.mocked(api.annotationsPage).mockResolvedValueOnce({ items: [], annotatable: false });

    const first = open();

    await first.user.type(first.input, "add annotation");
    await first.user.keyboard("{Enter}");

    expect(await screen.findByText("You may not add annotations.")).toBeInTheDocument();
    expect(opened).not.toHaveBeenCalled();
    cleanup();

    vi.mocked(api.annotationsPage).mockResolvedValueOnce({ items: [], annotatable: true });

    const second = open();

    await second.user.type(second.input, "add annotation");
    await second.user.keyboard("{Enter}");

    await waitFor(() => expect(opened).toHaveBeenCalled());
    expect(second.onClose).toHaveBeenCalled();
    window.removeEventListener("cetacean:add-annotation", opened);
  });
});
