import { ResourceTable } from "./ResourceTable";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

/**
 * Services as list_resources returns them: raw Docker Engine API records, with
 * capitalised field names. Getting this wrong in the widget is invisible to the
 * Go compiler, which is why the shape is spelled out here rather than mocked.
 */
const services = [
  {
    ID: "svc-b",
    Spec: { Name: "monitoring", TaskTemplate: { ContainerSpec: { Image: "prom/prometheus" } } },
  },
  {
    ID: "svc-a",
    Spec: { Name: "cetacean", TaskTemplate: { ContainerSpec: { Image: "cetacean:latest" } } },
  },
];

describe("ResourceTable", () => {
  it("renders a row per record, reading nested Docker fields", () => {
    render(
      <ResourceTable
        resourceType="services"
        records={services}
        total={services.length}
      />,
    );

    expect(screen.getByText("monitoring")).toBeInTheDocument();
    expect(screen.getByText("cetacean")).toBeInTheDocument();
    expect(screen.getByText("prom/prometheus")).toBeInTheDocument();
  });

  it("filters rows by the search box", () => {
    render(
      <ResourceTable
        resourceType="services"
        records={services}
        total={services.length}
      />,
    );

    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "ceta" } });

    expect(screen.queryByText("monitoring")).not.toBeInTheDocument();
    expect(screen.getByText("cetacean")).toBeInTheDocument();
  });

  it("matches on any column, not just the name", () => {
    render(
      <ResourceTable
        resourceType="services"
        records={services}
        total={services.length}
      />,
    );

    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "prom/" } });

    expect(screen.getByText("monitoring")).toBeInTheDocument();
    expect(screen.queryByText("cetacean")).not.toBeInTheDocument();
  });

  it("sorts when a column header is clicked, and reverses on a second click", () => {
    render(
      <ResourceTable
        resourceType="services"
        records={services}
        total={services.length}
      />,
    );

    fireEvent.click(screen.getByText(/^Name/));

    let rows = screen.getAllByRole("row").slice(1);
    expect(within(rows[0]!).getByText("cetacean")).toBeInTheDocument();

    fireEvent.click(screen.getByText(/^Name/));

    rows = screen.getAllByRole("row").slice(1);
    expect(within(rows[0]!).getByText("monitoring")).toBeInTheDocument();
  });

  it("says when it is showing a subset, so a truncated page is not read as the whole cluster", () => {
    render(
      <ResourceTable
        resourceType="services"
        records={services}
        total={57}
      />,
    );

    expect(screen.getByText("2 of 57 services")).toBeInTheDocument();
  });

  it("renders an em dash for a field the record does not carry", () => {
    render(
      <ResourceTable
        resourceType="services"
        records={[{ ID: "svc-c", Spec: { Name: "no-image" } }]}
        total={1}
      />,
    );

    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("falls back to a plain name/ID table for a type it has no columns for", () => {
    render(
      <ResourceTable
        resourceType="widgets"
        records={[{ ID: "w1", Spec: { Name: "future-type" } }]}
        total={1}
      />,
    );

    expect(screen.getByText("future-type")).toBeInTheDocument();
    expect(screen.getByText("w1")).toBeInTheDocument();
  });
});
