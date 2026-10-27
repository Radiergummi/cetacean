import { NetworksEditor } from "./NetworksEditor";
import { api } from "@/api/client";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { allNetworks: vi.fn<() => Promise<unknown>>() },
}));

vi.mock("@/hooks/useEditVersion", () => ({
  useEditVersion: () => ({ capture: () => {}, ifMatch: async () => undefined }),
}));

beforeEach(() => {
  vi.mocked(api.allNetworks).mockReset();
});

function renderEditor() {
  render(
    <NetworksEditor
      serviceId="svc1"
      networks={[{ target: "net1" }]}
      onSaved={() => {}}
      canEdit
    />,
    { wrapper: createWrapper(createTestQueryClient()) },
  );
}

describe("NetworksEditor", () => {
  it("names attached networks from the network list", async () => {
    vi.mocked(api.allNetworks).mockResolvedValue([{ Id: "net1", Name: "backend" }] as never);

    renderEditor();

    expect(await screen.findByText("backend")).toBeInTheDocument();
  });

  it("says so when the network list cannot be loaded", async () => {
    vi.mocked(api.allNetworks).mockRejectedValue(new Error("502"));

    renderEditor();
    fireEvent.click(screen.getByRole("button", { name: /edit/i }));
    fireEvent.click(screen.getByRole("button", { name: /add another/i }));

    expect(await screen.findByText(/Failed to load networks/)).toBeInTheDocument();
  });
});
