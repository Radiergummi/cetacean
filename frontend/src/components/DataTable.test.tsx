import DataTable from "./DataTable";
import type { Column } from "./DataTable";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

interface Item {
  id: string;
  name: string;
}

const columns: Column<Item>[] = [
  { header: "ID", cell: ({ id }) => id },
  { header: "Name", cell: ({ name }) => name },
];

const data: Item[] = [
  { id: "1", name: "Alpha" },
  { id: "2", name: "Beta" },
];

describe("DataTable", () => {
  it("renders headers", () => {
    render(
      <DataTable
        columns={columns}
        data={data}
        keyFn={({ id }) => id}
      />,
    );
    expect(screen.getByText("ID")).toBeInTheDocument();
    expect(screen.getByText("Name")).toBeInTheDocument();
  });

  it("renders rows", () => {
    render(
      <DataTable
        columns={columns}
        data={data}
        keyFn={({ id }) => id}
      />,
    );
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("Beta")).toBeInTheDocument();
  });

  it("calls onRowClick", () => {
    const onClick = vi.fn();
    render(
      <DataTable
        columns={columns}
        data={data}
        keyFn={({ id }) => id}
        onRowClick={onClick}
      />,
    );
    fireEvent.click(screen.getByText("Alpha"));
    expect(onClick).toHaveBeenCalledWith(data[0]);
  });

  it("renders empty table", () => {
    const { container } = render(
      <DataTable
        columns={columns}
        data={[]}
        keyFn={({ id }) => id}
      />,
    );
    expect(container.querySelectorAll("tbody tr")).toHaveLength(0);
  });

  it("renders sentinel when hasMore is true", () => {
    render(
      <DataTable
        columns={columns}
        data={data}
        keyFn={({ id }) => id}
        hasMore
        onLoadMore={() => {}}
      />,
    );
    expect(screen.getByTestId("load-more-sentinel")).toBeInTheDocument();
  });

  it("does not render sentinel when hasMore is false", () => {
    render(
      <DataTable
        columns={columns}
        data={data}
        keyFn={({ id }) => id}
        hasMore={false}
      />,
    );
    expect(screen.queryByTestId("load-more-sentinel")).not.toBeInTheDocument();
  });
});
