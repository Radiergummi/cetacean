import CollapsibleSection from "./CollapsibleSection";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

describe("CollapsibleSection", () => {
  // Page headers are the only h1, and panels inside a section are h3.
  it("titles its section with a second-level heading holding the toggle", () => {
    render(
      <MemoryRouter>
        <CollapsibleSection title="Labels">content</CollapsibleSection>
      </MemoryRouter>,
    );

    const heading = screen.getByRole("heading", { level: 2, name: "Labels" });

    expect(heading).toContainElement(screen.getByRole("button", { name: "Labels" }));
  });
});
