import MonitoringStatus from "./MonitoringStatus";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("MonitoringStatus", () => {
  it("doesn't repeat the heading's own message in the detail box", () => {
    // "Cannot reach Prometheus" already says this; a box underneath
    // repeating "prometheus unreachable" verbatim tells the reader nothing
    // the heading didn't.
    render(
      <MonitoringStatus
        status={{
          prometheusConfigured: true,
          prometheusReachable: false,
          queryable: true,
          error: "prometheus unreachable",
          nodeExporter: null,
          cadvisor: null,
        }}
      />,
    );

    expect(screen.getByText("Cannot reach Prometheus")).toBeInTheDocument();
    expect(screen.queryByText("prometheus unreachable")).not.toBeInTheDocument();
  });

  it("shows a cause that says more than the heading, such as a timeout", () => {
    render(
      <MonitoringStatus
        status={{
          prometheusConfigured: true,
          prometheusReachable: false,
          queryable: true,
          error: "prometheus query timed out",
          nodeExporter: null,
          cadvisor: null,
        }}
      />,
    );

    expect(screen.getByText("prometheus query timed out")).toBeInTheDocument();
  });

  it("stays quiet for a caller who may not query metrics", () => {
    const { container } = render(
      <MonitoringStatus
        status={{
          prometheusConfigured: true,
          prometheusReachable: false,
          queryable: false,
          error: "prometheus unreachable",
          nodeExporter: { targets: 0, nodes: 3 },
          cadvisor: null,
        }}
      />,
    );

    expect(container).toBeEmptyDOMElement();
  });
});
