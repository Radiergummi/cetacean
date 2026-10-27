import { CaddyPanel } from "./CaddyPanel";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: {
    patchServiceLabels: vi.fn<() => Promise<Record<string, string>>>(async () => ({})),
  },
  currentETag: vi.fn<() => Promise<string | undefined>>(async () => undefined),
}));

describe("CaddyPanel", () => {
  it("edits its labels in the raw view, having no form of its own", () => {
    render(
      <CaddyPanel
        integration={{ name: "caddy", enabled: true, caddyfile: "example.com {\n}\n" }}
        rawLabels={[["caddy", "example.com"]]}
        serviceId="s1"
        onSaved={vi.fn<() => void>()}
        editable
      />,
      { wrapper: createWrapper(createTestQueryClient()) },
    );

    expect(screen.getByText("Caddyfile")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /edit/i }));

    expect(screen.queryByText("Caddyfile")).toBeNull();
    expect(screen.getByDisplayValue("example.com")).toBeTruthy();
    expect(screen.getByRole("button", { name: /structured/i })).toHaveProperty("disabled", true);
  });
});
