import PageHeader from "./PageHeader";
import { render, screen } from "@testing-library/react";
import type { ReactElement } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect } from "vitest";

function renderWithRouter(ui: ReactElement, initialRoute = "/") {
  return render(<MemoryRouter initialEntries={[initialRoute]}>{ui}</MemoryRouter>);
}

describe("PageHeader", () => {
  it("renders title", () => {
    renderWithRouter(<PageHeader title="Nodes" />);
    expect(screen.getByText("Nodes")).toBeInTheDocument();
  });

  it("renders without breadcrumbs", () => {
    renderWithRouter(<PageHeader title="Nodes" />);
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  });

  it("renders breadcrumbs with links", () => {
    renderWithRouter(
      <PageHeader
        title="my-node"
        breadcrumbs={[{ label: "Nodes", to: "/nodes" }, { label: "my-node" }]}
      />,
    );
    const link = screen.getByText("Nodes");
    expect(link.closest("a")).toHaveAttribute("href", "/nodes");
    // Last crumb has no link - appears in both breadcrumb and title
    expect(screen.getAllByText("my-node")).toHaveLength(2);
  });

  it("renders breadcrumb without link as plain text", () => {
    renderWithRouter(
      <PageHeader
        title="Detail"
        breadcrumbs={[{ label: "Current" }]}
      />,
    );
    const el = screen.getByText("Current");
    expect(el.tagName).toBe("SPAN");
    expect(el.closest("a")).toBeNull();
  });

  it("shows feed button on resource pages", () => {
    renderWithRouter(<PageHeader title="Services" />, "/services");
    const link = screen.getByLabelText("Atom feed");
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute("href", "/services.atom");
  });

  it("links the search feed with the query after the suffix", () => {
    renderWithRouter(<PageHeader title="Search" />, "/search?q=web");
    expect(screen.getByLabelText("Atom feed")).toHaveAttribute("href", "/search.atom?q=web");
  });

  it("hides feed button on a resource's sub-pages", () => {
    renderWithRouter(<PageHeader title="Environment" />, "/services/abc/env");
    expect(screen.queryByLabelText("Atom feed")).not.toBeInTheDocument();
  });

  it("hides feed button on pages without feeds", () => {
    renderWithRouter(<PageHeader title="Cluster" />, "/cluster");
    expect(screen.queryByLabelText("Atom feed")).not.toBeInTheDocument();
  });

  // The title row truncates; anything wider than the screen must wrap instead.
  it("renders extra content outside the truncated title row", () => {
    renderWithRouter(
      <PageHeader
        title="web"
        extra={<span>extra content</span>}
      />,
    );

    expect(screen.getByText("extra content").closest(".truncate")).toBeNull();
  });
});
