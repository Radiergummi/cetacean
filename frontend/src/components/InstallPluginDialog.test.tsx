import InstallPluginDialog from "./InstallPluginDialog";
import { api } from "@/api/client";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: {
    pluginPrivileges: vi.fn<() => Promise<unknown>>(),
    installPlugin: vi.fn<() => Promise<unknown>>(),
  },
}));

// The privileges shown must be the ones of the plugin installed: a reference
// edited after the check goes back through the check.
describe("InstallPluginDialog", () => {
  it("asks for a new check when the reference changes after one", async () => {
    vi.mocked(api.pluginPrivileges).mockResolvedValue([] as never);
    render(
      <InstallPluginDialog
        open
        onOpenChange={() => {}}
        onInstalled={() => {}}
      />,
    );

    const input = screen.getByPlaceholderText("docker.io/library/plugin:latest");
    fireEvent.change(input, { target: { value: "vieux/sshfs:latest" } });
    fireEvent.click(screen.getByRole("button", { name: "Check Privileges" }));

    expect(await screen.findByRole("button", { name: "Install" })).toBeInTheDocument();

    fireEvent.change(input, { target: { value: "evil/plugin:latest" } });

    expect(screen.queryByRole("button", { name: "Install" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Check Privileges" })).toBeInTheDocument();
  });
});
