import GlobalSearch from "./GlobalSearch";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("GlobalSearch", () => {
  it("names its trigger button", () => {
    render(<GlobalSearch />);
    expect(screen.getByRole("button", { name: "Search" })).toBeInTheDocument();
  });
});
