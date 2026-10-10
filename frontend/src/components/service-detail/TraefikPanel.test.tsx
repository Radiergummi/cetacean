import { TraefikPanel } from "./TraefikPanel";
import { api } from "@/api/client";
import type { TraefikIntegration } from "@/api/types";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: {
    patchServiceLabels: vi.fn<() => Promise<Record<string, string>>>(async () => ({})),
  },
}));

function integrationWithPort(port: number): TraefikIntegration {
  return { name: "traefik", enabled: true, services: [{ name: "web", port }] };
}

describe("TraefikPanel", () => {
  it("saves nothing when the integration changes while the form is open", async () => {
    const rawLabels: [string, string][] = [
      ["traefik.http.services.web.loadbalancer.server.port", "8080"],
    ];
    const props = { rawLabels, serviceId: "s1", onSaved: vi.fn<() => void>(), editable: true };
    const { rerender } = render(
      <TraefikPanel
        integration={integrationWithPort(80)}
        {...props}
      />,
      { wrapper: createWrapper(createTestQueryClient()) },
    );

    fireEvent.click(screen.getByRole("button", { name: /edit/i }));

    rerender(
      <TraefikPanel
        integration={integrationWithPort(8080)}
        {...props}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(api.patchServiceLabels).toHaveBeenCalledWith("s1", []));
  });
});
