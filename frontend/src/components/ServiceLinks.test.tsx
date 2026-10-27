import ServiceLinks from "./ServiceLinks";
import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

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
});
