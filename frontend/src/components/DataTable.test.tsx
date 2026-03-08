import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import DataTable from "./DataTable";
import type { Column } from "./DataTable";

interface Item {
  id: string;
  name: string;
}

const columns: Column<Item>[] = [
  { header: "ID", cell: (item) => item.id },
  { header: "Name", cell: (item) => item.name },
];

const data: Item[] = [
  { id: "1", name: "Alpha" },
  { id: "2", name: "Beta" },
];

describe("DataTable", () => {
  it("renders headers", () => {
    render(<DataTable columns={columns} data={data} keyFn={(i) => i.id} />);
    expect(screen.getByText("ID")).toBeInTheDocument();
    expect(screen.getByText("Name")).toBeInTheDocument();
  });

  it("renders rows", () => {
    render(<DataTable columns={columns} data={data} keyFn={(i) => i.id} />);
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("Beta")).toBeInTheDocument();
  });

  it("calls onRowClick", () => {
    const onClick = vi.fn();
    render(<DataTable columns={columns} data={data} keyFn={(i) => i.id} onRowClick={onClick} />);
    fireEvent.click(screen.getByText("Alpha"));
    expect(onClick).toHaveBeenCalledWith(data[0]);
  });

  it("renders empty table", () => {
    const { container } = render(<DataTable columns={columns} data={[]} keyFn={(i) => i.id} />);
    expect(container.querySelectorAll("tbody tr")).toHaveLength(0);
  });
});
