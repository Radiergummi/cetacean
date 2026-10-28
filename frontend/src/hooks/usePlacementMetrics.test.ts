import type { PrometheusResponse } from "../api/types";
import { parseInstant } from "../lib/prometheusParser";
import { placementUsageKey, placementUsageSpec } from "./usePlacementMetrics";
import { describe, expect, it } from "vitest";

describe("placementUsageSpec", () => {
  it("files a series under the key a box looks it up by", () => {
    const response: PrometheusResponse = {
      data: {
        resultType: "vector",
        result: [
          {
            metric: {
              container_label_com_docker_swarm_service_id: "svc1",
              container_label_com_docker_swarm_node_id: "node1",
            },
            value: [1000, "12.5"],
          },
        ],
      },
    };

    expect(parseInstant(response, placementUsageSpec.labelKey)).toEqual([
      [placementUsageKey("svc1", "node1"), 12.5],
    ]);
  });

  it("sums only task containers, by service and node", () => {
    for (const { query } of placementUsageSpec.instant ?? []) {
      expect(query).toContain(
        "sum by (container_label_com_docker_swarm_service_id, container_label_com_docker_swarm_node_id)",
      );
      expect(query).toContain(`container_label_com_docker_swarm_task_id!=""`);
    }
  });
});
