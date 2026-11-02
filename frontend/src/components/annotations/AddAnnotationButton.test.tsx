import AddAnnotationButton from "./AddAnnotationButton";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const capabilities = vi.hoisted(() => ({ create: true, clusterWide: false }));

vi.mock("@/hooks/useAnnotationCapabilities", () => ({
  useAnnotationCapabilities: () => capabilities,
}));

const service = { type: "service", id: "svc1", name: "web" } as const;

function show(scope?: typeof service) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AddAnnotationButton scope={scope} />
    </QueryClientProvider>,
  );
}

describe("AddAnnotationButton", () => {
  it("opens the form with the page's resource", async () => {
    const user = userEvent.setup();

    capabilities.clusterWide = false;
    show(service);
    await user.click(screen.getByRole("button", { name: "Add annotation" }));

    expect(await screen.findByRole("radio", { name: "web" })).toBeChecked();
    expect(screen.queryByRole("radio", { name: "Cluster-wide" })).not.toBeInTheDocument();
  });

  it("adds cluster-wide only when allowed", async () => {
    const user = userEvent.setup();

    capabilities.clusterWide = true;
    show(service);
    await user.click(screen.getByRole("button", { name: "Add annotation" }));

    expect(await screen.findByRole("radio", { name: "Cluster-wide" })).toBeInTheDocument();
  });

  it("searches for a resource without one", async () => {
    const user = userEvent.setup();

    capabilities.clusterWide = false;
    show();
    await user.click(screen.getByRole("button", { name: "Add annotation" }));

    expect(await screen.findByPlaceholderText(/Search services/)).toBeInTheDocument();
  });
});
