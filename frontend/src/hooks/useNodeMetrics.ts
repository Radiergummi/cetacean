import { api } from "../api/client";
import type { PrometheusResponse } from "../api/types";
import { useInstanceResolver } from "./useInstanceResolver";
import { useMonitoringStatus } from "./useMonitoringStatus";
import { useCallback, useEffect, useState } from "react";

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
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
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
      // noinspection UnnecessaryLocalVariableJS
      const cpuRangeQuery = cpuQuery;

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
        api
          .metricsQueryRange(cpuRangeQuery, String(start), String(now), String(step))
          .catch((error) => {
            console.warn(error);
            return null;
          }),
      ]).then(([cpuResponse, memResponse, diskResponse, cpuRangeResponse]) => {
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

        parseInstant(cpuResponse)?.forEach(([instance, value]) => {
          ensure(instance).cpu = value;
        });
        parseInstant(memResponse)?.forEach(([instance, value]) => {
          ensure(instance).memory = value;
        });
        parseInstant(diskResponse)?.forEach(([instance, value]) => {
          ensure(instance).disk = value;
        });
        parseRange(cpuRangeResponse)?.forEach(([instance, values]) => {
          ensure(instance).cpuHistory = values;
        });

        setByInstance(map);
      });
    };

    doFetch();
    const interval = setInterval(doFetch, 30000);
    return () => {
      cancelled = true;

      clearInterval(interval);
    };
  }, [hasPrometheus]);

  const getForNode = useCallback(
    (hostname: string, address: string): NodeMetrics => {
      // Resolve hostname → instance via node_uname_info mapping
      const instance = resolve(hostname);

      if (instance && byInstance[instance]) {
        return byInstance[instance];
      }

      // Fallback: try matching by addr (works when IPs happen to match)
      for (const [key, metrics] of Object.entries(byInstance)) {
        if (key.startsWith(address + ":") || key === address) {
          return metrics;
        }
      }

      return emptyMetrics;
    },
    [byInstance, resolve],
  );

  return { getForNode, hasData: Object.keys(byInstance).length > 0 };
}

function parseInstant(resp: PrometheusResponse | null): [string, number][] | null {
  const results = resp?.data?.result;

  if (!results?.length) {
    return null;
  }

  return results.map(
    ({ metric, value }) => [metric?.instance || "", Number(value?.[1])] as [string, number],
  );
}

function parseRange(response: PrometheusResponse | null): [string, number[]][] | null {
  const results = response?.data?.result;

  if (!results?.length) {
    return null;
  }

  return results.map(
    ({ metric, values }) =>
      [metric?.instance || "", (values || []).map((value) => Number(value[1]))] as [
        string,
        number[],
      ],
  );
}
