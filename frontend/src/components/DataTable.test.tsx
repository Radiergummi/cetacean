import DataTable from "./DataTable";
import type { Column } from "./DataTable";
import { render, screen, fireEvent } from "@testing-library/react";
import { useState } from "react";
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
    const onClick = vi.fn<(item: Item) => void>();
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

  it("exposes a sortable header as a button and announces the sort direction", () => {
    const toggle = vi.fn<() => void>();
    const sortable: Column<Item>[] = [
      { header: "ID", cell: ({ id }) => id, onHeaderClick: toggle, sortDirection: "ascending" },
      { header: "Name", cell: ({ name }) => name },
    ];

    render(
      <DataTable
        label="Items"
        columns={sortable}
        data={data}
        keyFn={({ id }) => id}
      />,
    );

    const header = screen.getByRole("columnheader", { name: "ID" });
    expect(header).toHaveAttribute("aria-sort", "ascending");

    // Sorting was a click handler on the `<th>`, so a keyboard had no route to
    // it at all. It is a real button now, which Enter and Space both activate.
    const button = screen.getByRole("button", { name: "ID" });
    fireEvent.click(button);
    expect(toggle).toHaveBeenCalledTimes(1);

    expect(screen.getByRole("columnheader", { name: "Name" })).not.toHaveAttribute("aria-sort");
  });

  it("points aria-activedescendant at the selected row", () => {
    const { container } = render(
      <DataTable
        label="Items"
        columns={columns}
        data={data}
        keyFn={({ id }) => id}
        onRowClick={() => {}}
      />,
    );

    const grid = screen.getByRole("grid");
    expect(grid).not.toHaveAttribute("aria-activedescendant");

    fireEvent.keyDown(grid, { key: "ArrowDown" });

    const active = grid.getAttribute("aria-activedescendant");
    expect(active).toBeTruthy();
    expect(container.querySelector(`#${CSS.escape(active!)}`)).toHaveAttribute("data-selected");
  });

  // The list hooks hand down a fresh array on every render, and an SSE event
  // rewrites the rows it holds. Neither means the person moved their cursor.
  it("keeps the keyboard selection across a re-render that changes the data identity", () => {
    function Harness() {
      const [tick, setTick] = useState(0);

      return (
        <>
          <button
            type="button"
            onClick={() => setTick(tick + 1)}
          >
            rerender
          </button>
          <DataTable
            columns={columns}
            data={data.map((item) => ({ ...item }))}
            keyFn={({ id }) => id}
            onRowClick={() => {}}
          />
        </>
      );
    }

    const { container } = render(<Harness />);
    const grid = screen.getByRole("grid");

    fireEvent.keyDown(grid, { key: "ArrowDown" });
    expect(container.querySelectorAll("[data-selected]")).toHaveLength(1);

    fireEvent.click(screen.getByText("rerender"));
    expect(container.querySelectorAll("[data-selected]")).toHaveLength(1);
  });

  // The spacer rows share the tbody, so nth-child parity there tracks the
  // scroll position instead of the row's place in the collection.
  it("stripes virtualized rows by their index, not their render position", () => {
    const many: Item[] = Array.from({ length: 150 }, (_, index) => ({
      id: String(index),
      name: `Row ${index}`,
    }));

    // The virtualizer sizes its window from the scroll element's offset box,
    // which jsdom reports as zero — it would render no rows at all.
    const height = vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(600);
    const width = vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(800);

    const table = () => (
      <DataTable
        columns={columns}
        data={many}
        keyFn={({ id }) => id}
      />
    );
    const { container, rerender } = render(table());

    // The virtualizer measures in an effect and publishes its range on the
    // pass after, so a fresh element has to go through before any row exists.
    rerender(table());

    const rows = container.querySelectorAll<HTMLElement>("tbody tr[data-index]");
    expect(rows.length).toBeGreaterThan(0);

    for (const row of rows) {
      const index = Number(row.dataset.index);

      expect(row.hasAttribute("data-stripe")).toBe(index % 2 === 1);
    }

    // Every row a virtual body renders opts out of the positional rule,
    // spacers included — a striped spacer would draw a band of its own.
    for (const row of container.querySelectorAll("tbody tr")) {
      expect(row.hasAttribute("data-virtual-row")).toBe(true);
    }

    height.mockRestore();
    width.mockRestore();
  });

  it("drops a selection that the data no longer holds", () => {
    function Harness() {
      const [rows, setRows] = useState(data);

      return (
        <>
          <button
            type="button"
            onClick={() => setRows([])}
          >
            clear
          </button>
          <DataTable
            columns={columns}
            data={rows}
            keyFn={({ id }) => id}
            onRowClick={() => {}}
          />
        </>
      );
    }

    const { container } = render(<Harness />);
    const grid = screen.getByRole("grid");

    fireEvent.keyDown(grid, { key: "ArrowDown" });
    fireEvent.keyDown(grid, { key: "ArrowDown" });
    expect(container.querySelectorAll("[data-selected]")).toHaveLength(1);

    fireEvent.click(screen.getByText("clear"));
    expect(container.querySelectorAll("[data-selected]")).toHaveLength(0);
  });
});
