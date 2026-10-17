import ErrorIndex from "./ErrorIndex";
import { api } from "@/api/client";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { errorDefinitions: vi.fn<() => Promise<unknown>>() },
}));

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

    render(<ErrorIndex />, { wrapper: createWrapper(createTestQueryClient()) });

    expect(await screen.findByText("ACL001")).toBeInTheDocument();
  });
});
