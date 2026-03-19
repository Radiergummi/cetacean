import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import QueryResultTable from "./QueryResultTable";

describe("QueryResultTable", () => {
  it("renders metric name, labels, and value", () => {
    const data = {
      resultType: "vector" as const,
      result: [
        {
          metric: { __name__: "up", instance: "localhost:9090", job: "prometheus" },
          value: [1710000000, "1"] as [number, string],
        },
      ],
    };
    render(<QueryResultTable data={data} />);
    expect(screen.getByText("up")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("renders empty state when no results", () => {
    const data = { resultType: "vector" as const, result: [] };
    render(<QueryResultTable data={data} />);
    expect(screen.getByText(/no results/i)).toBeInTheDocument();
  });
});
