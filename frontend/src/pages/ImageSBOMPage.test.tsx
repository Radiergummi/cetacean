import ImageSBOMPage from "./ImageSBOMPage";
import { ApiError } from "@/api/client";
import type { ImageProvenance, ImageSBOM } from "@/api/types";
import { createTestQueryClient } from "@/test/mocks";
import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: {
    sbom: vi.fn<() => Promise<ImageSBOM>>(),
    provenance: vi.fn<() => Promise<ImageProvenance>>(),
  },
}));

import { api } from "@/api/client";
const sbom = vi.mocked(api.sbom);
const provenance = vi.mocked(api.provenance);

const debian: ImageSBOM = {
  platform: "linux/amd64",
  generators: ["Tool: syft-v1.42.3"],
  summary: { total: 3, licensed: 2, ecosystems: { deb: 2, golang: 1 } },
  packages: [
    {
      name: "acl",
      version: "2.3.2-2",
      ecosystem: "deb",
      license: "GPL-2.0-only",
      purl: "pkg:deb/debian/acl@2.3.2-2",
      foundIn: "dpkg db: /var/lib/dpkg/status",
    },
    {
      name: "aom",
      version: "3.12.1",
      ecosystem: "deb",
      license: "BSD-2-Clause",
      purl: "pkg:deb/debian/aom@3.12.1",
      foundIn: "dpkg db: /var/lib/dpkg/status",
    },
    {
      name: "cel.dev/expr",
      version: "v0.25.1",
      ecosystem: "golang",
      license: "",
      purl: "pkg:golang/cel.dev/expr@v0.25.1",
      foundIn: "go module information: /bin/syft-scanner",
    },
  ],
};

function renderPage(path = "/services/svc1/sbom?platform=linux/amd64") {
  render(
    <QueryClientProvider client={createTestQueryClient()}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route
            path="/services/:id/sbom"
            element={<ImageSBOMPage kind="service" />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function rows() {
  return screen.getAllByRole("row").slice(1);
}

beforeEach(() => {
  sbom.mockReset();
  provenance.mockReset();
});

describe("ImageSBOMPage", () => {
  it("searches by name and package URL", async () => {
    sbom.mockResolvedValue(debian);
    renderPage();

    await screen.findByText("acl");
    fireEvent.change(screen.getByPlaceholderText(/Search/), { target: { value: "pkg:golang" } });

    expect(rows()).toHaveLength(1);
    expect(within(rows()[0] as HTMLElement).getByText("cel.dev/expr")).toBeInTheDocument();
  });

  it("filters by ecosystem and license", async () => {
    sbom.mockResolvedValue(debian);
    renderPage();

    await screen.findByText("acl");
    fireEvent.change(screen.getByLabelText("Ecosystem"), { target: { value: "deb" } });
    expect(rows()).toHaveLength(2);

    fireEvent.change(screen.getByLabelText("License"), { target: { value: "BSD-2-Clause" } });
    expect(rows()).toHaveLength(1);
    expect(screen.getByText("aom")).toBeInTheDocument();
  });

  it("shows where each package was found", async () => {
    sbom.mockResolvedValue(debian);
    renderPage();

    expect(await screen.findByText("go module information: /bin/syft-scanner")).toBeInTheDocument();
  });

  it("explains an SBOM whose Go modules carry no license", async () => {
    sbom.mockResolvedValue({
      ...debian,
      summary: { total: 1, licensed: 0, ecosystems: { golang: 1 } },
      packages: [debian.packages[2]!],
    });
    renderPage();

    expect(
      await screen.findByText(/Go modules carry no license in Syft's SBOMs/),
    ).toBeInTheDocument();
  });

  it("links the downloads", async () => {
    sbom.mockResolvedValue(debian);
    renderPage();

    expect(await screen.findByRole("link", { name: /CSV/ })).toHaveAttribute(
      "href",
      "/services/svc1/sbom.csv?platform=linux%2Famd64",
    );
    expect(screen.getByRole("link", { name: /SPDX/ })).toHaveAttribute(
      "href",
      "/services/svc1/sbom.spdx?platform=linux%2Famd64",
    );
  });

  it("offers the platforms when one must be named", async () => {
    sbom.mockRejectedValue(
      new ApiError("/api/errors/ATT003", "Platform Required", 400, "name one"),
    );
    provenance.mockResolvedValue({
      image: "app:1",
      status: "available",
      platforms: [
        {
          platform: "linux/amd64",
          manifestDigest: "a",
          runningTasks: 0,
          dockerfile: false,
          sbom: { digest: "s", size: 1, tooLarge: false },
        },
        {
          platform: "linux/arm64",
          manifestDigest: "b",
          runningTasks: 0,
          dockerfile: false,
          sbom: { digest: "t", size: 1, tooLarge: false },
        },
        { platform: "linux/arm/v7", manifestDigest: "c", runningTasks: 0, dockerfile: false },
      ],
    });
    renderPage("/services/svc1/sbom");

    expect(await screen.findByRole("link", { name: "linux/arm64" })).toHaveAttribute(
      "href",
      "/services/svc1/sbom?platform=linux%2Farm64",
    );
    expect(screen.queryByRole("link", { name: "linux/arm/v7" })).toBeNull();
  });
});
