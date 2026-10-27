import { useInstanceResolver } from "./useInstanceResolver";
import { useMetricsMap, type MetricsMapSpec } from "./useMetricsMap";
import { isPrometheusReady, useMonitoringStatus } from "./useMonitoringStatus";
import { useCallback, useMemo } from "react";

export interface NodeMetrics {
  cpu: number | null;
  memory: number | null;
  disk: number | null;
  cpuHistory: number[];
}

const emptyMetrics: NodeMetrics = {
  cpu: null,
  memory: null,
  disk: null,
  cpuHistory: [],
};

const cpuQuery = `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`;
const memQuery = `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`;
const diskQuery = `max by (instance) ((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"}) * 100)`;

type InstantField = NonNullable<MetricsMapSpec<NodeMetrics>["instant"]>[number];
type RangeField = NonNullable<MetricsMapSpec<NodeMetrics>["range"]>[number];

export const nodeCpu: InstantField = {
  query: cpuQuery,
  assign: (metrics, value) => {
    metrics.cpu = value;
  },
};

export const nodeMemory: InstantField = {
  query: memQuery,
  assign: (metrics, value) => {
    metrics.memory = value;
  },
};

const nodeDisk: InstantField = {
  query: diskQuery,
  assign: (metrics, value) => {
    metrics.disk = value;
  },
};

const nodeCpuHistory: RangeField = {
  query: cpuQuery,
  assign: (metrics, value) => {
    metrics.cpuHistory = value;
  },
};

const everyInstant = [nodeCpu, nodeMemory, nodeDisk];
const everyRange = [nodeCpuHistory];

// Fetches per-instance node-exporter metrics, by default every field, and maps
// them by instance label. Nodes are matched to instances via
// useInstanceResolver (hostname-based). Never fetches without Prometheus.
export function useNodeMetrics({
  instant = everyInstant,
  range = everyRange,
  enabled: requested = true,
}: {
  instant?: readonly InstantField[] | undefined;
  range?: readonly RangeField[] | undefined;
  enabled?: boolean | undefined;
} = {}) {
  const enabled = isPrometheusReady(useMonitoringStatus()) && requested;
  const { resolve } = useInstanceResolver(enabled);
  const queries = [...instant, ...range].map(({ query }) => query).join("|");
  const byInstance = useMetricsMap(
    `node-metrics:${queries}`,
    {
      labelKey: "instance",
      empty: (): NodeMetrics => ({ ...emptyMetrics, cpuHistory: [] }),
      instant,
      range,
    },
    enabled,
  );

  const instanceEntries = useMemo(() => Object.entries(byInstance), [byInstance]);

  const getForNode = useCallback(
    (hostname: string, address: string | undefined): NodeMetrics => {
      // Resolve hostname → instance via node_uname_info mapping
      const instance = resolve(hostname);

      if (instance && byInstance[instance]) {
        return byInstance[instance];
      }

      // Fallback: match by address (IP) or hostname against the instance label
      const short = hostname.split(".")[0];

      for (const [key, metrics] of instanceEntries) {
        if (address && (key.startsWith(address + ":") || key === address)) {
          return metrics;
        }

        const host = key.split(":")[0] ?? key;

        if (host === hostname || host === short || host.split(".")[0] === short) {
          return metrics;
        }
      }

      return emptyMetrics;
    },
    [byInstance, instanceEntries, resolve],
  );

  return { getForNode, hasData: instanceEntries.length > 0 };
}
