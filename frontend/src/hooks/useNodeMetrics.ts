import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";

export interface NodeMetrics {
  cpu: number | null;
  memory: number | null;
  disk: number | null;
  cpuHistory: number[];
}

const EMPTY: NodeMetrics = { cpu: null, memory: null, disk: null, cpuHistory: [] };

// Fetches per-instance metrics from Prometheus and maps them by IP address.
// Nodes are matched to instances via node.Status.Addr.
export function useNodeMetrics() {
  const [byAddr, setByAddr] = useState<Record<string, NodeMetrics>>({});

  const fetchAll = useCallback(() => {
    // Instant queries — per instance
    const cpuQ = `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`;
    const memQ = `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`;
    const diskQ = `max by (instance) ((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"}) * 100)`;

    // Range query for CPU sparklines — 1h, ~2min steps = 30 points
    const now = Math.floor(Date.now() / 1000);
    const start = now - 3600;
    const step = 120;
    const cpuRangeQ = `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`;

    Promise.all([
      api.metricsQuery(cpuQ).catch(() => null),
      api.metricsQuery(memQ).catch(() => null),
      api.metricsQuery(diskQ).catch(() => null),
      api.metricsQueryRange(cpuRangeQ, String(start), String(now), String(step)).catch(() => null),
    ]).then(([cpuResp, memResp, diskResp, cpuRangeResp]) => {
      const map: Record<string, NodeMetrics> = {};

      const ensure = (addr: string) => {
        if (!map[addr]) map[addr] = { ...EMPTY, cpuHistory: [] };
        return map[addr];
      };

      parseInstant(cpuResp)?.forEach(([addr, val]) => {
        ensure(addr).cpu = val;
      });
      parseInstant(memResp)?.forEach(([addr, val]) => {
        ensure(addr).memory = val;
      });
      parseInstant(diskResp)?.forEach(([addr, val]) => {
        ensure(addr).disk = val;
      });
      parseRange(cpuRangeResp)?.forEach(([addr, values]) => {
        ensure(addr).cpuHistory = values;
      });

      setByAddr(map);
    });
  }, []);

  useEffect(() => {
    fetchAll();
    const interval = setInterval(fetchAll, 30000);
    return () => clearInterval(interval);
  }, [fetchAll]);

  const getForNode = useCallback(
    (nodeAddr: string): NodeMetrics => {
      // Direct match
      if (byAddr[nodeAddr]) return byAddr[nodeAddr];
      // Match by IP prefix (instance is "ip:port", nodeAddr is just "ip")
      for (const key of Object.keys(byAddr)) {
        if (key.startsWith(nodeAddr + ":") || key === nodeAddr) {
          return byAddr[key];
        }
      }
      // Fallback: return the first instance with actual data
      // (handles mismatched IPs in single-node setups like OrbStack)
      for (const entry of Object.values(byAddr)) {
        if (entry.cpu != null) return entry;
      }
      return EMPTY;
    },
    [byAddr],
  );

  return { getForNode, hasData: Object.keys(byAddr).length > 0 };
}

function instanceAddr(instance: string): string {
  // "10.0.0.2:9100" -> "10.0.0.2"
  const i = instance.lastIndexOf(":");
  return i > 0 ? instance.slice(0, i) : instance;
}

function parseInstant(resp: any): [string, number][] | null {
  const results = resp?.data?.result;
  if (!results?.length) return null;
  return results.map(
    (r: any) => [instanceAddr(r.metric?.instance || ""), Number(r.value?.[1])] as [string, number],
  );
}

function parseRange(resp: any): [string, number[]][] | null {
  const results = resp?.data?.result;
  if (!results?.length) return null;
  return results.map(
    (r: any) =>
      [instanceAddr(r.metric?.instance || ""), (r.values || []).map((v: any) => Number(v[1]))] as [
        string,
        number[],
      ],
  );
}
