import { fakeApp, resetFakeHost, sendToolInput } from "../test/widgetHost";
import { MetricsWidget } from "./metrics/MetricsWidget";
import { RecommendationsWidget } from "./recommendations/RecommendationsWidget";
import { TableWidget } from "./table/TableWidget";
import { TopologyWidget } from "./topology/TopologyWidget";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock(
  "@modelcontextprotocol/ext-apps/react",
  async () => (await import("../test/widgetHost")).extAppsMock,
);

beforeEach(() => {
  resetFakeHost();
});

/**
 * Renders a widget, holds it until the host sends the tool input, and returns
 * the arguments of the one tool call it then makes.
 */
async function callFor(widget: ReactElement, input: Record<string, unknown>) {
  render(widget);

  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(fakeApp.callServerTool).not.toHaveBeenCalled();

  sendToolInput(input);

  await vi.waitFor(() => expect(fakeApp.callServerTool).toHaveBeenCalledTimes(1));

  return fakeApp.callServerTool.mock.calls[0]?.[0];
}

describe("widget tool input", () => {
  it("lists the type and filters find was called with", async () => {
    const request = await callFor(<TableWidget />, {
      type: "nodes",
      query: "worker",
      label: "zone=a",
      raw: true,
    });

    expect(request).toEqual({
      name: "find",
      arguments: { type: "nodes", query: "worker", label: "zone=a" },
    });
  });

  it("draws the view and node get_topology was called with", async () => {
    const request = await callFor(<TopologyWidget />, { view: "drain-impact", node: "n1" });

    expect(request).toEqual({
      name: "get_topology",
      arguments: { view: "drain-impact", node: "n1" },
    });
  });

  it("shows the severity get_recommendations was called with", async () => {
    const request = await callFor(<RecommendationsWidget />, { severity: "critical" });

    expect(request).toEqual({
      name: "get_recommendations",
      arguments: { severity: "critical" },
    });
  });

  it("charts what get_metrics was called with", async () => {
    const request = await callFor(<MetricsWidget />, { target: "service", id: "web" });

    expect(request).toEqual({
      name: "get_metrics",
      arguments: { target: "service", id: "web", metric: "cpu", range: "1h" },
    });
  });

  it("ranks the cluster with the top, by and range get_metrics was called with", async () => {
    const request = await callFor(<MetricsWidget />, {
      target: "cluster",
      top: 3,
      by: "node",
      metric: "memory",
      range: "24h",
    });

    expect(request).toEqual({
      name: "get_metrics",
      arguments: { target: "cluster", metric: "memory", top: 3, by: "node", range: "24h" },
    });
  });
});
