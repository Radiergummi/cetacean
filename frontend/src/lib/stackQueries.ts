const stack = "container_label_com_docker_stack_namespace";
const service = "container_label_com_docker_swarm_service_name";

/**
 * Returns StackDrillDownChart props for CPU and memory usage by stack.
 * Pass `extraFilter` to scope to a subset (e.g. a specific node).
 */
export function stackResourceCharts(extraFilter = "") {
  const filter = extraFilter ? `,${extraFilter}` : "";

  return {
    cpu: {
      title: "CPU Usage (by Stack)",
      stackQuery: `topk(10, sum by (${stack})(rate(container_cpu_usage_seconds_total{${stack}!=""${filter}}[5m])) * 100)`,
      serviceQueryTemplate: `sum by (${service})(rate(container_cpu_usage_seconds_total{${stack}="<STACK>",${service}!=""${filter}}[5m])) * 100`,
      unit: "%" as const,
      yMin: 0,
      stackable: true,
    },
    memory: {
      title: "Memory Usage (by Stack)",
      stackQuery: `topk(10, sum by (${stack})(container_memory_usage_bytes{${stack}!=""${filter}}))`,
      serviceQueryTemplate: `sum by (${service})(container_memory_usage_bytes{${stack}="<STACK>",${service}!=""${filter}})`,
      unit: "bytes" as const,
      yMin: 0,
      stackable: true,
    },
  };
}
