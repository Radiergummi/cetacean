import { placementUsageSpec } from "../hooks/usePlacementMetrics";
import { buildDataset } from "./dataset";
import { handleInstantQuery } from "./prometheus";
import { describe, expect, it } from "vitest";

const dataset = buildDataset();

describe("demo container queries", () => {
  it("answers the placement queries per service and node", () => {
    for (const { query } of placementUsageSpec.instant ?? []) {
      const [series] = handleInstantQuery(query, dataset).result;

      expect(series?.metric).toHaveProperty("container_label_com_docker_swarm_node_id");
      expect(series?.metric).toHaveProperty("container_label_com_docker_swarm_service_id");
    }
  });

  it("answers a query filtered by node with per-service series", () => {
    const query = `sum by (container_label_com_docker_swarm_service_name)(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_node_id="n1"}[5m]))`;
    const [series] = handleInstantQuery(query, dataset).result;

    expect(series?.metric).toHaveProperty("container_label_com_docker_swarm_service_name");
  });
});
