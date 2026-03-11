import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import ResourceGauge from "./ResourceGauge";

describe("ResourceGauge", () => {
  it("renders label", () => {
    render(<ResourceGauge label="CPU" value={50} />);
    expect(screen.getByText("CPU")).toBeInTheDocument();
  });

  it("renders percentage value", () => {
    render(<ResourceGauge label="CPU" value={75} />);
    expect(screen.getByText("75%")).toBeInTheDocument();
  });

  it("renders em dash for null value", () => {
    render(<ResourceGauge label="CPU" value={null} />);
    expect(screen.getByText("\u2014")).toBeInTheDocument();
  });

  it("clamps value to 0-100", () => {
    render(<ResourceGauge label="CPU" value={150} />);
    expect(screen.getByText("100%")).toBeInTheDocument();
  });

  it("renders subtitle when provided", () => {
    render(<ResourceGauge label="CPU" value={50} subtitle="4 cores" />);
    expect(screen.getByText("4 cores")).toBeInTheDocument();
  });
});
