import ContainerImage from "./ContainerImage";
import type { ImageUpdate } from "@/api/types";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: { updateServiceImage: vi.fn<() => Promise<void>>() },
}));

const runningDigest = "sha256:" + "a".repeat(64);
const newerDigest = "sha256:" + "b".repeat(64);

const imageUpdate: ImageUpdate = {
  image: "nginx:1.27",
  currentDigest: runningDigest,
  latestDigest: newerDigest,
  suggestedImage: `nginx:1.27@${newerDigest}`,
  checkedAt: "2026-10-01T12:00:00Z",
};

describe("ContainerImage", () => {
  it("says nothing about a newer image when there is none", () => {
    render(<ContainerImage image={`nginx:1.27@${runningDigest}`} />);

    expect(screen.queryByText("Newer image")).not.toBeInTheDocument();
  });

  it("marks an image whose tag names a newer one", () => {
    render(
      <ContainerImage
        image={`nginx:1.27@${runningDigest}`}
        imageUpdate={imageUpdate}
      />,
    );

    expect(screen.getByText("Newer image")).toBeInTheDocument();
    expect(screen.getByText(/A newer image exists for this tag/)).toBeInTheDocument();
  });

  it("suggests the newer digest in the update popover", async () => {
    render(
      <ContainerImage
        image={`nginx:1.27@${runningDigest}`}
        serviceId="svc1"
        canEdit
        imageUpdate={imageUpdate}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Update image" }));

    await waitFor(() => {
      expect(screen.getByPlaceholderText("image:tag")).toHaveValue(imageUpdate.suggestedImage);
    });
  });

  it("suggests the tag alone without a newer image", async () => {
    render(
      <ContainerImage
        image={`nginx:1.27@${runningDigest}`}
        serviceId="svc1"
        canEdit
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Update image" }));

    await waitFor(() => {
      expect(screen.getByPlaceholderText("image:tag")).toHaveValue("nginx:1.27");
    });
  });
});
