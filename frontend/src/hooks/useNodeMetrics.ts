import { api } from "../api/client";
import type { PrometheusResponse } from "../api/types";
import { useInstanceResolver } from "./useInstanceResolver";
import { useMonitoringStatus } from "./useMonitoringStatus";
import { useState, useEffect, useCallback } from "react";

export interface NodeMetrics {
  cpu: number | null;
  memory: number | null;
  disk: number | null;
  cpuHistory: number[];
}

const EMPTY: NodeMetrics = { cpu: null, memory: null, disk: null, cpuHistory: [] };

// Fetches per-instance metrics from Prometheus and maps them by instance label.
// Nodes are matched to instances via useInstanceResolver (hostname-based).
export function useNodeMetrics() {
  const monitoring = useMonitoringStatus();
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
  const { resolve } = useInstanceResolver();
  const [byInstance, setByInstance] = useState<Record<string, NodeMetrics>>({});

  useEffect(() => {
    if (!hasPrometheus) return;
    let cancelled = false;

    const doFetch = () => {
      const cpuQ = `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`;
      const memQ = `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`;
      const diskQ = `max by (instance) ((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"}) * 100)`;
      const now = Math.floor(Date.now() / 1000);
      const start = now - 3600;
      const step = 120;
      const cpuRangeQ = cpuQ;

      Promise.all([
        api.metricsQuery(cpuQ).catch(() => null),
        api.metricsQuery(memQ).catch(() => null),
        api.metricsQuery(diskQ).catch(() => null),
        api
          .metricsQueryRange(cpuRangeQ, String(start), String(now), String(step))
          .catch(() => null),
      ]).then(([cpuResp, memResp, diskResp, cpuRangeResp]) => {
        if (cancelled) return;
        const map: Record<string, NodeMetrics> = {};

        const ensure = (instance: string) => {
          if (!map[instance]) map[instance] = { ...EMPTY, cpuHistory: [] };
          return map[instance];
        };

        parseInstant(cpuResp)?.forEach(([instance, val]) => {
          ensure(instance).cpu = val;
        });
        parseInstant(memResp)?.forEach(([instance, val]) => {
          ensure(instance).memory = val;
        });
        parseInstant(diskResp)?.forEach(([instance, val]) => {
          ensure(instance).disk = val;
        });
        parseRange(cpuRangeResp)?.forEach(([instance, values]) => {
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
    (hostname: string, addr: string): NodeMetrics => {
      // Resolve hostname → instance via node_uname_info mapping
      const instance = resolve(hostname);
      if (instance && byInstance[instance]) return byInstance[instance];

      // Fallback: try matching by addr (works when IPs happen to match)
      for (const [key, metrics] of Object.entries(byInstance)) {
        if (key.startsWith(addr + ":") || key === addr) {
          return metrics;
        }
      }

      return EMPTY;
    },
    [byInstance, resolve],
  );

  return { getForNode, hasData: Object.keys(byInstance).length > 0 };
}

function parseInstant(resp: PrometheusResponse | null): [string, number][] | null {
  const results = resp?.data?.result;
  if (!results?.length) return null;
  return results.map((r) => [r.metric?.instance || "", Number(r.value?.[1])] as [string, number]);
}

function parseRange(resp: PrometheusResponse | null): [string, number[]][] | null {
  const results = resp?.data?.result;
  if (!results?.length) return null;
  return results.map(
    (r) =>
      [r.metric?.instance || "", (r.values || []).map((v) => Number(v[1]))] as [string, number[]],
  );
}
