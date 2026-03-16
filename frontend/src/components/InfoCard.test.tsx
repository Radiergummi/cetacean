import InfoCard from "./InfoCard";
import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

describe("InfoCard", () => {
  it("renders label and value", () => {
    render(
      <InfoCard
        label="Role"
        value="manager"
      />,
    );
    expect(screen.getByText("Role")).toBeInTheDocument();
    expect(screen.getByText("manager")).toBeInTheDocument();
  });

  it("renders em dash when value is undefined", () => {
    render(<InfoCard label="Status" />);
    expect(screen.getByText("\u2014")).toBeInTheDocument();
  });

  it("renders em dash when value is empty string", () => {
    render(
      <InfoCard
        label="Status"
        value=""
      />,
    );
    expect(screen.getByText("\u2014")).toBeInTheDocument();
  });
});
