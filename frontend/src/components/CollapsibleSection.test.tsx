import CollapsibleSection from "./CollapsibleSection";
import { render, screen, waitFor } from "@testing-library/react";
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

  it("opens and scrolls to a collapsed section the URL's hash names, without storing it", async () => {
    localStorage.setItem("section:services:metrics", "0");

    render(
      <MemoryRouter initialEntries={["/services/s1#metrics"]}>
        <CollapsibleSection
          title="Metrics"
          id="metrics"
        >
          charts
        </CollapsibleSection>
      </MemoryRouter>,
    );

    expect(await screen.findByText("charts")).toBeInTheDocument();
    await waitFor(() => expect(Element.prototype.scrollIntoView).toHaveBeenCalled());
    expect(localStorage.getItem("section:services:metrics")).toBe("0");
  });
});
