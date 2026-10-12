import type { MonitoringStatus } from "../api/types";
import { isCadvisorReady, isNodeExporterReady, isPrometheusReady } from "./useMonitoringStatus";
import { describe, expect, it } from "vitest";

const healthy: MonitoringStatus = {
  prometheusConfigured: true,
  prometheusReachable: true,
  queryable: true,
  nodeExporter: { targets: 3, nodes: 3 },
  cadvisor: { targets: 3, nodes: 3 },
};

describe("isPrometheusReady", () => {
  it("is ready when Prometheus answers and the caller may query it", () => {
    expect(isPrometheusReady(healthy)).toBe(true);
  });

  // The proxy answers 403 to such a caller, so every chart would fail.
  it("is not ready for a caller who may not query metrics", () => {
    const denied = { ...healthy, queryable: false };

    expect(isPrometheusReady(denied)).toBe(false);
    expect(isCadvisorReady(denied)).toBe(false);
    expect(isNodeExporterReady(denied)).toBe(false);
  });
});
