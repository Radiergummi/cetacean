import { api } from "../api/client";
import { parseInstant, parseRange } from "../lib/prometheusParser";
import { useInstanceResolver } from "./useInstanceResolver";
import { isPrometheusReady, useMonitoringStatus } from "./useMonitoringStatus";
import { useCallback, useEffect, useMemo, useState } from "react";

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

// Fetches per-instance metrics from Prometheus and maps them by instance label.
// Nodes are matched to instances via useInstanceResolver (hostname-based).
export function useNodeMetrics() {
  const monitoring = useMonitoringStatus();
  const hasPrometheus = isPrometheusReady(monitoring);
  const { resolve } = useInstanceResolver();
  const [byInstance, setByInstance] = useState<Record<string, NodeMetrics>>({});

  useEffect(() => {
    if (!hasPrometheus) {
      return;
    }

    let cancelled = false;

    const doFetch = () => {
      const cpuQuery = `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`;
      const memQuery = `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`;
      const diskQuery = `max by (instance) ((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"}) * 100)`;
      const now = Math.floor(Date.now() / 1000);
      const start = now - 3600;
      const step = 120;

      Promise.all([
        api.metricsQuery(cpuQuery).catch((error) => {
          console.warn(error);
          return null;
        }),
        api.metricsQuery(memQuery).catch((error) => {
          console.warn(error);
          return null;
        }),
        api.metricsQuery(diskQuery).catch((error) => {
          console.warn(error);
          return null;
        }),
        api.metricsQueryRange(cpuQuery, String(start), String(now), String(step)).catch((error) => {
          console.warn(error);
          return null;
        }),
      ])
        .then(([cpuResponse, memResponse, diskResponse, cpuRangeResponse]) => {
          if (cancelled) {
            return;
          }

          const map: Record<string, NodeMetrics> = {};

          const ensure = (instance: string) => {
            if (!map[instance]) {
              map[instance] = { ...emptyMetrics, cpuHistory: [] };
            }

            return map[instance];
          };

          parseInstant(cpuResponse, "instance")?.forEach(([instance, value]) => {
            ensure(instance).cpu = value;
          });
          parseInstant(memResponse, "instance")?.forEach(([instance, value]) => {
            ensure(instance).memory = value;
          });
          parseInstant(diskResponse, "instance")?.forEach(([instance, value]) => {
            ensure(instance).disk = value;
          });
          parseRange(cpuRangeResponse, "instance")?.forEach(([instance, values]) => {
            ensure(instance).cpuHistory = values;
          });

          setByInstance(map);
        })
        .catch(console.warn);
    };

    doFetch();
    const interval = setInterval(doFetch, 30000);
    return () => {
      cancelled = true;

      clearInterval(interval);
    };
  }, [hasPrometheus]);

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
