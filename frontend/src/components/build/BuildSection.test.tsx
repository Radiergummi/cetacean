import BuildSection from "./BuildSection";
import type { ImageProvenance } from "@/api/types";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { provenance: vi.fn<() => Promise<ImageProvenance>>() },
}));

import { api } from "@/api/client";
const provenance = vi.mocked(api.provenance);

beforeEach(() => {
  provenance.mockReset();
});

afterEach(() => vi.useRealTimers());

const available: ImageProvenance = {
  image: "app:1@sha256:abc",
  status: "available",
  platforms: [
    {
      platform: "linux/amd64",
      manifestDigest: "sha256:amd",
      runningTasks: 1,
      dockerfile: false,
      provenance: { builderID: undefined } as never,
    },
    {
      platform: "linux/arm64",
      manifestDigest: "sha256:arm",
      runningTasks: 3,
      dockerfile: true,
      sbom: { digest: "sha256:sbom", size: 2048, tooLarge: false },
      provenance: {
        source: {
          repository: "https://github.com/docker/buildkit-syft-scanner.git",
          ref: "refs/tags/v1.12.1",
          commit: "1c368712e124e90ee912ec8615afa78974ddad19",
          repositoryUrl: "https://github.com/docker/buildkit-syft-scanner",
          commitUrl: "https://github.com/docker/buildkit-syft-scanner/commit/1c36871",
        },
        dockerfilePath: "Dockerfile",
        ci: {
          actor: "crazy-max",
          event: "push",
          ref: "refs/tags/v1.12.1",
          workflow: ".github/workflows/ci.yaml",
          runId: "40359464897",
          runUrl:
            "https://github.com/docker/buildkit-syft-scanner/actions/runs/40359464897/attempts/1",
          workflowUrl:
            "https://github.com/docker/buildkit-syft-scanner/blob/422dfad/.github/workflows/ci.yaml",
        },
      },
    },
  ],
};

function renderSection(kind: "service" | "task" = "service") {
  return render(
    <BuildSection
      kind={kind}
      id="svc1"
    />,
    { wrapper: createWrapper(createTestQueryClient()) },
  );
}

async function shown(response: ImageProvenance) {
  provenance.mockResolvedValue(response);
  const view = renderSection();

  await waitFor(() => expect(provenance).toHaveBeenCalled());

  return view;
}

describe("BuildSection", () => {
  it("selects the platform with most running tasks", async () => {
    await shown(available);

    expect(await screen.findByRole("combobox", { name: "Platform" })).toHaveValue("linux/arm64");
  });

  it("renders the CI line", async () => {
    await shown(available);

    expect(await screen.findByText(/Pushed by crazy-max/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "ci.yaml" })).toHaveAttribute(
      "href",
      available.platforms?.[1]?.provenance?.ci?.workflowUrl,
    );
    expect(screen.getByRole("link", { name: /40359464897/ })).toHaveAttribute(
      "href",
      available.platforms?.[1]?.provenance?.ci?.runUrl,
    );
  });

  it("links the SBOM and Dockerfile pages", async () => {
    await shown(available);

    const dockerfile = "/services/svc1/dockerfile?platform=linux%2Farm64";

    expect(await screen.findByRole("link", { name: "SBOM" })).toHaveAttribute(
      "href",
      "/services/svc1/sbom?platform=linux%2Farm64",
    );
    expect(screen.getByRole("link", { name: "Show Dockerfile" })).toHaveAttribute(
      "href",
      dockerfile,
    );
    expect(screen.getByRole("link", { name: "Dockerfile" })).toHaveAttribute("href", dockerfile);
  });

  it("states read as facts", async () => {
    for (const status of ["none", "unpinned", "private", "missing", "unavailable"] as const) {
      const view = await shown({
        image: "app:1",
        status,
        retryAfter: new Date(Date.now() + 60_000).toISOString(),
      });

      expect(await screen.findByTestId("build-status")).toBeInTheDocument();
      expect(screen.queryByRole("alert")).not.toBeInTheDocument();
      view.unmount();
    }
  });

  it("offers a retry once retryAfter has passed", async () => {
    await shown({
      image: "app:1",
      status: "unavailable",
      retryAfter: new Date(Date.now() + 1_000).toISOString(),
    });

    const retry = await screen.findByRole("button", { name: "Retry" });
    expect(retry).toBeDisabled();

    await waitFor(() => expect(retry).toBeEnabled(), { timeout: 3_000 });

    fireEvent.click(retry);
    await waitFor(() => expect(provenance).toHaveBeenCalledTimes(2));
  });

  it("says when one platform's provenance couldn't be read", async () => {
    await shown({
      ...available,
      platforms: [
        {
          platform: "linux/amd64",
          manifestDigest: "sha256:amd",
          runningTasks: 1,
          dockerfile: false,
          provenanceStatus: "unavailable",
        },
      ],
    });

    expect(
      await screen.findByText(/provenance couldn't be read: The registry couldn't be reached/),
    ).toBeInTheDocument();
  });

  it("never says verified", async () => {
    const { container } = await shown(available);

    await screen.findByText(/Pushed by/);
    expect(container.textContent).not.toMatch(/verif/i);
  });

  it("renders nothing when disabled", async () => {
    const { container } = await shown({ image: "app:1", status: "disabled" });

    await waitFor(() => expect(container.querySelector("h2")).toBeNull());
  });
});
