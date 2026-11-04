import ActivitySection from "./ActivitySection";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/hooks/useClusterAnnotatable", () => ({
  useClusterAnnotatable: () => false,
}));

describe("ActivitySection", () => {
  it("offers to annotate even without activity", () => {
    render(
      <MemoryRouter>
        <ActivitySection
          entries={[]}
          annotate={{ type: "node", id: "n1", name: "worker" }}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("Recent Activity")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add annotation" })).toBeInTheDocument();
  });

  it("stays hidden for a caller who may not annotate", () => {
    const { container } = render(<ActivitySection entries={[]} />);

    expect(container).toBeEmptyDOMElement();
  });
});
