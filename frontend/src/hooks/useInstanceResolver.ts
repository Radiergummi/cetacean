import { api } from "../api/client";
import { isPrometheusReady, useMonitoringStatus } from "./useMonitoringStatus";
import { useQuery } from "@tanstack/react-query";
import { useCallback } from "react";

const noInstances: Record<string, string> = {};

/**
 * Resolves Docker Swarm node hostnames to Prometheus instance labels.
 *
 * In overlay-networked setups, the Prometheus `instance` label (overlay IP)
 * doesn't match the node's swarm address. This hook fetches `node_uname_info`
 * and builds a nodename->instance mapping so metrics can be queried per-node.
 *
 * Requires node-exporter to run with `hostname: "{{.Node.Hostname}}"` so
 * that `node_uname_info.nodename` reports the Docker Swarm node hostname.
 */
export function useInstanceResolver(enabled = true) {
  const monitoring = useMonitoringStatus();
  const hasPrometheus = isPrometheusReady(monitoring);

  const { data: byHostname = noInstances } = useQuery({
    queryKey: ["instance-resolver"],
    queryFn: async () => {
      const response = await api.metricsQuery("node_uname_info");
      const map: Record<string, string> = {};

      for (const { metric } of response.data?.result ?? []) {
        const nodename = metric?.["nodename"];
        const instance = metric?.["instance"];

        if (nodename && instance) {
          map[nodename] = instance;
        }
      }

      return map;
    },
    enabled: hasPrometheus && enabled,
    refetchInterval: 60_000,
    refetchIntervalInBackground: true,
    staleTime: 60_000,
  });

  /**
   * Resolve a Docker Swarm hostname to a Prometheus instance label.
   * Tries exact match first, then prefix match (hostname without a domain).
   */
  const resolve = useCallback(
    (hostname: string): string | null => {
      // Exact match
      if (byHostname[hostname]) {
        return byHostname[hostname];
      }

      // Prefix match: "app-prod-whale-1.skate-forel.ts.net" -> "app-prod-whale-1"
      const short = hostname.split(".")[0];

      if (short && byHostname[short]) {
        return byHostname[short];
      }

      // Reverse: a key that is a leading run of the hostname's labels
      for (const [name, instance] of Object.entries(byHostname)) {
        if (hostname.startsWith(`${name}.`)) {
          return instance;
        }
      }

      return null;
    },
    [byHostname],
  );

  return {
    resolve,
    hasData: Object.keys(byHostname).length > 0,
  };
}
