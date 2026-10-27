import ImageDockerfilePage from "./ImageDockerfilePage";
import type { ImageDockerfile } from "@/api/types";
import { formatBytes } from "@/lib/format";
import { createTestQueryClient } from "@/test/mocks";
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { dockerfile: vi.fn<() => Promise<ImageDockerfile>>() },
}));

import { api } from "@/api/client";
const dockerfile = vi.mocked(api.dockerfile);

// The probe image: testdata/probe.Dockerfile in internal/attestations.
const probe: ImageDockerfile = {
  platform: "linux/arm64",
  path: "Dockerfile",
  lines: [
    "FROM alpine:3.20 AS deps",
    "RUN apk add --no-cache jq",
    "",
    "FROM alpine:3.20",
    "RUN apk add --no-cache curl",
    "RUN dd if=/dev/zero of=/big bs=1M count=5",
    "COPY --from=deps /usr/bin/jq /usr/local/bin/jq",
    "COPY hello.txt /hello.txt",
    "RUN echo done > /done",
  ],
  steps: [
    {
      ranges: [
        [1, 1],
        [4, 4],
      ],
      base: true,
      layers: [{ digest: "sha256:l1", size: 4092319 }],
    },
    { ranges: [[5, 5]], base: false, layers: [{ digest: "sha256:l2", size: 2565002 }] },
    { ranges: [[6, 6]], base: false, layers: [{ digest: "sha256:l3", size: 5194 }] },
    { ranges: [[7, 7]], base: false, layers: [{ digest: "sha256:l4", size: 144808 }] },
    { ranges: [[8, 8]], base: false, layers: [{ digest: "sha256:l5", size: 110 }] },
    { ranges: [[9, 9]], base: false, layers: [{ digest: "sha256:l6", size: 105 }] },
  ],
  unattributed: [],
  totalSize: 6807538,
  baseSize: 4092319,
};

function renderPage(response: ImageDockerfile) {
  dockerfile.mockResolvedValue(response);
  render(
    <QueryClientProvider client={createTestQueryClient()}>
      <MemoryRouter initialEntries={["/tasks/task1/dockerfile?platform=linux/arm64"]}>
        <Routes>
          <Route
            path="/tasks/:id/dockerfile"
            element={<ImageDockerfilePage kind="task" />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => dockerfile.mockReset());

describe("ImageDockerfilePage", () => {
  it("annotates each line with the layers it produced", async () => {
    renderPage(probe);

    expect(await screen.findByTestId("line-6")).toHaveTextContent(formatBytes(5194));
    expect(screen.getByTestId("line-6")).toHaveTextContent("RUN dd if=/dev/zero");
    expect(screen.getByTestId("line-3")).not.toHaveTextContent(/B$/);
  });

  it("shows a step's layers on each of its lines", async () => {
    renderPage(probe);

    expect(await screen.findByTestId("line-1")).toHaveTextContent(formatBytes(4092319));
    expect(screen.getByTestId("line-4")).toHaveTextContent(formatBytes(4092319));
  });

  it("totals the image and its base", async () => {
    renderPage(probe);

    expect(await screen.findByText(new RegExp(formatBytes(6807538)))).toBeInTheDocument();
    expect(
      screen.getByText(new RegExp(`${formatBytes(4092319)} from the base image`)),
    ).toBeInTheDocument();
  });

  it("lists layers no line produced", async () => {
    renderPage({ ...probe, unattributed: [{ digest: "sha256:stray", size: 7 }] });

    expect(await screen.findByText(/sha256:stray/)).toBeInTheDocument();
  });

  it("makes the heaviest line stand out", async () => {
    renderPage(probe);

    const heaviest = await screen.findByTestId("bar-4");
    expect(heaviest.style.width).toBe("100%");
    expect(parseFloat(screen.getByTestId("bar-6").style.width)).toBeLessThan(1);
  });

  it("links the raw Dockerfile", async () => {
    renderPage(probe);

    expect(await screen.findByRole("link", { name: /Download/ })).toHaveAttribute(
      "href",
      "/tasks/task1/dockerfile.txt?platform=linux%2Farm64",
    );
  });
});
