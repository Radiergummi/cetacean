import Licenses from "./Licenses";
import { api } from "@/api/client";
import type { LicensesResponse } from "@/api/types";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: { licenses: vi.fn<() => Promise<LicensesResponse>>() },
}));

const sample = {
  components: [
    { name: "github.com/foo/bar", version: "v1.0.0", ecosystem: "go", licenses: [{ id: "MIT" }] },
    { name: "react", version: "19.0.0", ecosystem: "npm", licenses: [{ id: "MIT" }] },
    { name: "react-dom", version: "19.0.0", ecosystem: "npm", licenses: [{ id: "MIT" }] },
  ],
};

describe("Licenses", () => {
  beforeEach(() => {
    (api.licenses as ReturnType<typeof vi.fn>).mockResolvedValue(sample);
  });

  it("renders all components then filters by ecosystem", async () => {
    render(<Licenses />, { wrapper: createWrapper(createTestQueryClient()) });

    await waitFor(() => expect(screen.getByText("react")).toBeInTheDocument());
    expect(screen.getByText("github.com/foo/bar")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^npm/i }));

    expect(screen.queryByText("github.com/foo/bar")).not.toBeInTheDocument();
    expect(screen.getByText("react-dom")).toBeInTheDocument();
  });

  it("filters by search query", async () => {
    render(<Licenses />, { wrapper: createWrapper(createTestQueryClient()) });

    await waitFor(() => expect(screen.getByText("react-dom")).toBeInTheDocument());

    fireEvent.change(screen.getByPlaceholderText(/search by name/i), {
      target: { value: "react-dom" },
    });

    expect(screen.queryByText("github.com/foo/bar")).not.toBeInTheDocument();
    expect(screen.getByText("react-dom")).toBeInTheDocument();
  });

  it("filters the grid by license", async () => {
    (api.licenses as ReturnType<typeof vi.fn>).mockResolvedValue({
      components: [
        { name: "alpha", ecosystem: "npm", licenses: [{ id: "MIT" }] },
        { name: "beta", ecosystem: "npm", licenses: [{ id: "Apache-2.0" }] },
      ],
    });

    render(<Licenses />, { wrapper: createWrapper(createTestQueryClient()) });

    await waitFor(() => expect(screen.getByText("alpha")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: /all licenses/i }));
    fireEvent.click(screen.getByRole("button", { name: /Apache-2\.0/ }));

    expect(screen.queryByText("alpha")).not.toBeInTheDocument();
    expect(screen.getByText("beta")).toBeInTheDocument();
  });

  it("combines the license filter with search", async () => {
    (api.licenses as ReturnType<typeof vi.fn>).mockResolvedValue({
      components: [
        { name: "alpha", ecosystem: "npm", licenses: [{ id: "MIT" }] },
        { name: "alpine", ecosystem: "npm", licenses: [{ id: "Apache-2.0" }] },
        { name: "beta", ecosystem: "npm", licenses: [{ id: "MIT" }] },
      ],
    });

    render(<Licenses />, { wrapper: createWrapper(createTestQueryClient()) });

    await waitFor(() => expect(screen.getByText("alpha")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: /all licenses/i }));
    fireEvent.click(screen.getByRole("button", { name: /^MIT/ }));

    fireEvent.change(screen.getByPlaceholderText(/search by name/i), {
      target: { value: "alp" },
    });

    // "alpine" matches the search but not the MIT license filter; "beta" matches
    // the license but not the search. Only "alpha" satisfies both, proving the
    // two filters narrow together rather than one overriding the other.
    expect(screen.queryByText("alpine")).not.toBeInTheDocument();
    expect(screen.queryByText("beta")).not.toBeInTheDocument();
    expect(screen.getByText("alpha")).toBeInTheDocument();
  });
});
