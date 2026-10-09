import ErrorIndex from "./ErrorIndex";
import { api } from "@/api/client";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { errorDefinitions: vi.fn<() => Promise<unknown>>() },
}));

// Through the shared client, so an expired session redirects to sign-in here
// as it does everywhere else.
describe("ErrorIndex", () => {
  it("lists the definitions the client returns", async () => {
    vi.mocked(api.errorDefinitions).mockResolvedValue([
      {
        code: "ACL001",
        title: "Access Denied",
        status: 403,
        description: "You do not have permission.",
        suggestion: "Check your grants.",
      },
    ]);

    render(
      <MemoryRouter>
        <ErrorIndex />
      </MemoryRouter>,
    );

    expect(await screen.findByText("ACL001")).toBeInTheDocument();
  });
});
