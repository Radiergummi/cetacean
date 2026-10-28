import { GantryPanel } from "./GantryPanel";
import type { GantryIntegration } from "@/api/types";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: {
    patchServiceLabels: vi.fn<() => Promise<Record<string, string>>>(async () => ({})),
  },
  currentETag: vi.fn<() => Promise<string | undefined>>(async () => undefined),
}));

function renderPanel(integration: GantryIntegration) {
  render(
    <GantryPanel
      integration={integration}
      rawLabels={[]}
      serviceId="s1"
      onSaved={vi.fn<() => void>()}
      editable
    />,
    { wrapper: createWrapper(createTestQueryClient()) },
  );
}

describe("GantryPanel", () => {
  it("says Gantry's defaults apply when no override is set", () => {
    renderPanel({ name: "gantry", enabled: true, labels: ["gantry.services.excluded"] });

    expect(screen.getByText(/defaults apply/i)).toBeTruthy();
  });

  it("offers a manifest command Gantry doesn't know, so the select shows it", () => {
    renderPanel({
      name: "gantry",
      enabled: true,
      labels: ["gantry.manifest.cmd"],
      manifestCmd: "docker",
    });

    fireEvent.click(screen.getByRole("button", { name: /edit/i }));

    expect(screen.getByLabelText<HTMLSelectElement>(/manifest command/i).value).toBe("docker");
  });
});
