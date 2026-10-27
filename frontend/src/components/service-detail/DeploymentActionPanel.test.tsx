import { DeploymentActionPanel } from "./DeploymentActionPanel";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: {
    patchServiceLabels: vi.fn<() => Promise<Record<string, string>>>(async () => ({})),
  },
  currentETag: vi.fn<() => Promise<string | undefined>>(async () => undefined),
}));

const sha = "0123456789abcdef0123456789abcdef01234567";

describe("DeploymentActionPanel", () => {
  it("links the repository, the commit and the workflow run", () => {
    render(
      <DeploymentActionPanel
        integration={{
          name: "docker-swarm-deployment-action",
          enabled: true,
          labels: ["com.matchory.deployment.commit", "com.matchory.deployment.run-url"],
          repository: "acme/shop",
          repositoryUrl: "https://github.com/acme/shop",
          commit: sha,
          commitUrl: `https://github.com/acme/shop/commit/${sha}`,
          runUrl: "https://github.com/acme/shop/actions/runs/123",
        }}
        rawLabels={[]}
        serviceId="s1"
        onSaved={vi.fn<() => void>()}
      />,
      { wrapper: createWrapper(createTestQueryClient()) },
    );

    expect(screen.getByText("acme/shop").closest("a")?.getAttribute("href")).toBe(
      "https://github.com/acme/shop",
    );
    expect(screen.getByText("0123456").closest("a")?.getAttribute("href")).toBe(
      `https://github.com/acme/shop/commit/${sha}`,
    );
    expect(screen.getByText("#123").closest("a")?.getAttribute("href")).toBe(
      "https://github.com/acme/shop/actions/runs/123",
    );
    expect(screen.queryByText("Monitor")).toBeNull();
  });

  it("shows the settings a service overrides", () => {
    render(
      <DeploymentActionPanel
        integration={{
          name: "docker-swarm-deployment-action",
          enabled: true,
          labels: ["com.matchory.deployment.monitor", "com.matchory.deployment.monitor-timeout"],
          monitor: false,
          monitorTimeoutSeconds: 900,
        }}
        rawLabels={[]}
        serviceId="s1"
        onSaved={vi.fn<() => void>()}
      />,
      { wrapper: createWrapper(createTestQueryClient()) },
    );

    expect(screen.getByText("No")).toBeTruthy();
    expect(screen.getByText("900s")).toBeTruthy();
    expect(screen.queryByText("Health check warnings")).toBeNull();
  });
});
