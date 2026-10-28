import { AttestedBadge } from "./AttestedBadge";
import type { ImageProvenance } from "@/api/types";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { provenance: vi.fn<() => Promise<ImageProvenance>>() },
}));

import { api } from "@/api/client";
const provenance = vi.mocked(api.provenance);

function renderBadge(response: ImageProvenance) {
  provenance.mockResolvedValue(response);

  return render(
    <AttestedBadge
      kind="service"
      id="svc1"
    />,
    { wrapper: createWrapper(createTestQueryClient()) },
  );
}

beforeEach(() => {
  provenance.mockReset();
});

describe("AttestedBadge", () => {
  it("marks an image with attestations", async () => {
    renderBadge({ image: "app:1@sha256:abc", status: "available", platforms: [] });

    expect(await screen.findByText(/Attested/)).toBeInTheDocument();
  });

  it("shows nothing for an image without", async () => {
    const { container } = renderBadge({ image: "app:1", status: "none" });

    await waitFor(() => expect(provenance).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });
});
