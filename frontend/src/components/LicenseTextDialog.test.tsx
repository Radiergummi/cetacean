import LicenseTextDialog from "./LicenseTextDialog";
import { api } from "@/api/client";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const component = {
  name: "@floating-ui/core",
  ecosystem: "npm",
  licenses: [{ id: "MIT" }],
  textId: "aaaa",
  noticeId: "bbbb",
};

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("LicenseTextDialog", () => {
  it("fetches nothing until it is opened", () => {
    const licenseText = vi.spyOn(api, "licenseText").mockResolvedValue("MIT License");

    render(
      <LicenseTextDialog
        component={component}
        open={false}
        onOpenChange={() => {}}
      />,
      { wrapper: createWrapper(createTestQueryClient(), { withRouter: false }) },
    );

    expect(licenseText).not.toHaveBeenCalled();
  });

  it("renders the license and the notice as separate sections", async () => {
    vi.spyOn(api, "licenseText").mockImplementation(async (id: string) =>
      id === "aaaa" ? "MIT License body" : "NOTICE body",
    );

    render(
      <LicenseTextDialog
        component={component}
        open
        onOpenChange={() => {}}
      />,
      { wrapper: createWrapper(createTestQueryClient(), { withRouter: false }) },
    );

    await waitFor(() => expect(screen.getByText("MIT License body")).toBeInTheDocument());

    // The distinction is legal, not cosmetic: a NOTICE is not part of the
    // license text and must not read as though it were.
    expect(screen.getByText("NOTICE body")).toBeInTheDocument();
    expect(screen.getByText(/^NOTICE$/)).toBeInTheDocument();
  });

  it("reports a failed fetch instead of showing an empty dialog", async () => {
    vi.spyOn(api, "licenseText").mockRejectedValue(new Error("nope"));

    render(
      <LicenseTextDialog
        component={{ ...component, noticeId: undefined }}
        open
        onOpenChange={() => {}}
      />,
      { wrapper: createWrapper(createTestQueryClient(), { withRouter: false }) },
    );

    await waitFor(() => expect(screen.getByText(/could not load/i)).toBeInTheDocument());
  });
});
