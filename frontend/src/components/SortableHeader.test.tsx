import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import SortableHeader from "./SortableHeader";

function renderInTable(ui: React.ReactElement) {
  return render(
    <table>
      <thead>
        <tr>{ui}</tr>
      </thead>
    </table>,
  );
}

describe("SortableHeader", () => {
  it("renders label", () => {
    renderInTable(<SortableHeader label="Name" sortKey="name" sortDir="asc" onToggle={vi.fn()} />);
    expect(screen.getByText("Name")).toBeInTheDocument();
  });

  it("calls onToggle with sort key on click", () => {
    const onToggle = vi.fn();
    renderInTable(<SortableHeader label="Name" sortKey="name" sortDir="asc" onToggle={onToggle} />);
    fireEvent.click(screen.getByText("Name").closest("th")!);
    expect(onToggle).toHaveBeenCalledWith("name");
  });

  it("shows inactive icon when not active", () => {
    const { container } = renderInTable(
      <SortableHeader
        label="Name"
        sortKey="name"
        activeSortKey="other"
        sortDir="asc"
        onToggle={vi.fn()}
      />,
    );
    // ChevronsUpDown has opacity-30 class
    expect(container.querySelector(".opacity-30")).toBeInTheDocument();
  });
});
