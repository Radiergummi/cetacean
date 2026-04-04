import { useInstanceResolver } from "./useInstanceResolver";
import { useMetricsMap } from "./useMetricsMap";
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

const spec = {
  labelKey: "instance",
  empty: (): NodeMetrics => ({ ...emptyMetrics, cpuHistory: [] }),
  instant: [
    {
      query: cpuQuery,
      assign: (m: NodeMetrics, v: number) => {
        m.cpu = v;
      },
    },
    {
      query: memQuery,
      assign: (m: NodeMetrics, v: number) => {
        m.memory = v;
      },
    },
    {
      query: diskQuery,
      assign: (m: NodeMetrics, v: number) => {
        m.disk = v;
      },
    },
  ],
  range: [
    {
      query: cpuQuery,
      assign: (m: NodeMetrics, v: number[]) => {
        m.cpuHistory = v;
      },
    },
  ],
} as const;

// Fetches per-instance metrics from Prometheus and maps them by instance label.
// Nodes are matched to instances via useInstanceResolver (hostname-based).
export function useNodeMetrics() {
  const monitoring = useMonitoringStatus();
  const hasPrometheus = isPrometheusReady(monitoring);
  const { resolve } = useInstanceResolver();
  const byInstance = useMetricsMap("node-metrics", spec, hasPrometheus);

  const instanceEntries = useMemo(() => Object.entries(byInstance), [byInstance]);

  const getForNode = useCallback(
    (hostname: string, address: string): NodeMetrics => {
      // Resolve hostname → instance via node_uname_info mapping
      const instance = resolve(hostname);

      if (instance && byInstance[instance]) {
        return byInstance[instance];
      }

      // Fallback: match by address (IP) or hostname against the instance label
      const short = hostname.split(".")[0];

      for (const [key, metrics] of instanceEntries) {
        if (key.startsWith(address + ":") || key === address) {
          return metrics;
        }

        const host = key.split(":")[0];

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
