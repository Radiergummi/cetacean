import { api } from "../api/client";
import { useMonitoringStatus } from "./useMonitoringStatus";
import { useCallback, useEffect, useState } from "react";

/**
 * Resolves Docker Swarm node hostnames to Prometheus instance labels.
 *
 * In overlay-networked setups, the Prometheus `instance` label (overlay IP)
 * doesn't match the node's swarm address. This hook fetches `node_uname_info`
 * and builds a nodename→instance mapping so metrics can be queried per-node.
 *
 * Requires node-exporter to run with `hostname: "{{.Node.Hostname}}"` so
 * that `node_uname_info.nodename` reports the Docker Swarm node hostname.
 */
export function useInstanceResolver() {
  const monitoring = useMonitoringStatus();
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
  // Map of nodename → instance (e.g. "app-prod-whale-1" → "10.100.9.27:9100")
  const [byHostname, setByHostname] = useState<Record<string, string>>({});

  const fetch_ = useCallback(() => {
    api
      .metricsQuery("node_uname_info")
      .then((response) => {
        const map: Record<string, string> = {};

        for (const result of response.data.result) {
          const nodename = result.metric.nodename;
          const instance = result.metric.instance;

          if (nodename && instance) {
            map[nodename] = instance;
          }
        }

        setByHostname(map);
      })
      .catch(console.warn);
  }, []);

  useEffect(() => {
    if (!hasPrometheus) {
      return;
    }

    fetch_();

    const interval = setInterval(fetch_, 60_000);

    return () => clearInterval(interval);
  }, [fetch_, hasPrometheus]);

  /**
   * Resolve a Docker Swarm hostname to a Prometheus instance label.
   * Tries exact match first, then prefix match (hostname without domain).
   */
  const resolve = useCallback(
    (hostname: string): string | null => {
      // Exact match
      if (byHostname[hostname]) {
        return byHostname[hostname];
      }

      // Prefix match: "app-prod-whale-1.skate-forel.ts.net" → "app-prod-whale-1"
      const short = hostname.split(".")[0];

      if (short && byHostname[short]) {
        return byHostname[short];
      }

      // Reverse: check if any key is a prefix of the hostname
      for (const [name, instance] of Object.entries(byHostname)) {
        if (hostname.startsWith(name)) {
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
