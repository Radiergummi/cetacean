import ResourceCard from "./ResourceCard";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

describe("ResourceCard", () => {
  // The list pages wrap each card in a plain block, so a link left inline
  // breaks its border across the lines it wraps over.
  it("renders a linked card as a block", () => {
    render(
      <MemoryRouter>
        <ResourceCard
          title="web"
          to="/services/s1"
          meta={["replicated", "3/3"]}
        />
      </MemoryRouter>,
    );

    expect(screen.getByRole("link").className.split(" ")).toContain("block");
  });
});
