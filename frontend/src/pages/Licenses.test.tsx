import { api } from "@/api/client";
import type { LicensesResponse } from "@/api/types";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import Licenses from "./Licenses";

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
    render(<MemoryRouter><Licenses /></MemoryRouter>);

    await waitFor(() => expect(screen.getByText("react")).toBeInTheDocument());
    expect(screen.getByText("github.com/foo/bar")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^npm/i }));

    expect(screen.queryByText("github.com/foo/bar")).not.toBeInTheDocument();
    expect(screen.getByText("react-dom")).toBeInTheDocument();
  });

  it("filters by search query", async () => {
    render(<MemoryRouter><Licenses /></MemoryRouter>);

    await waitFor(() => expect(screen.getByText("react-dom")).toBeInTheDocument());

    fireEvent.change(screen.getByPlaceholderText(/search by name/i), {
      target: { value: "react-dom" },
    });

    expect(screen.queryByText("github.com/foo/bar")).not.toBeInTheDocument();
    expect(screen.getByText("react-dom")).toBeInTheDocument();
  });
});
