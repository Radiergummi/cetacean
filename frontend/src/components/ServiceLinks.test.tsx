import ServiceLinks from "./ServiceLinks";
import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

describe("ServiceLinks", () => {
  it("renders an href as a link opening in a new tab", () => {
    render(
      <ServiceLinks
        links={[{ id: "grafana", title: "Dashboard", href: "https://grafana.example.com" }]}
      />,
    );

    const link = screen.getByRole("link", { name: /Dashboard/ });

    expect(link).toHaveAttribute("href", "https://grafana.example.com");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("renders a text value as plain text, never a link", () => {
    render(
      <ServiceLinks links={[{ id: "oncall", title: "On call", text: "javascript:alert(1)" }]} />,
    );

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText(/javascript:alert\(1\)/)).toBeInTheDocument();
  });

  it("renders nothing without links", () => {
    const { container } = render(<ServiceLinks links={[]} />);

    expect(container).toBeEmptyDOMElement();
  });

  // A stack merges only exact duplicates, so one id and URL can arrive under two titles.
  it("keys each entry by every field", () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});

    render(
      <ServiceLinks
        links={[
          { id: "repo", title: "Code", href: "https://git.example.com" },
          { id: "repo", title: "repo", href: "https://git.example.com" },
        ]}
      />,
    );

    expect(screen.getAllByRole("link")).toHaveLength(2);
    expect(error).not.toHaveBeenCalled();
    error.mockRestore();
  });

  it("truncates a long text entry and keeps its full value in a tooltip", () => {
    render(<ServiceLinks links={[{ id: "note", title: "Note", text: "x".repeat(300) }]} />);

    const entry = screen.getByText(/^Note: x+$/);

    expect(entry).toHaveClass("truncate");
    expect(entry).toHaveAttribute("title", `Note: ${"x".repeat(300)}`);
  });
});
