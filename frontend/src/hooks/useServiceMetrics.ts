import { api } from "../api/client";
import { parseInstant, parseRange } from "../lib/prometheusParser";
import { isCadvisorReady, useMonitoringStatus } from "./useMonitoringStatus";
import { useEffect, useState, useCallback } from "react";

export interface ServiceMetrics {
  cpu: number | null;
  cpuHistory: number[];
  memory: number | null;
  memoryHistory: number[];
}

const emptyMetrics: ServiceMetrics = {
  cpu: null,
  cpuHistory: [],
  memory: null,
  memoryHistory: [],
};

const serviceLabel = "container_label_com_docker_swarm_service_name";

export function useServiceMetrics() {
  const monitoring = useMonitoringStatus();
  const hasCadvisor = isCadvisorReady(monitoring);
  const [byService, setByService] = useState<Record<string, ServiceMetrics>>({});

  useEffect(() => {
    if (!hasCadvisor) {
      return;
    }

    let cancelled = false;

    const doFetch = () => {
      const cpuQuery = `sum by (${serviceLabel})(rate(container_cpu_usage_seconds_total{${serviceLabel}!=""}[5m])) * 100`;
      const memQuery = `sum by (${serviceLabel})(container_memory_usage_bytes{${serviceLabel}!=""})`;
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
        api.metricsQueryRange(cpuQuery, String(start), String(now), String(step)).catch((error) => {
          console.warn(error);
          return null;
        }),
        api.metricsQueryRange(memQuery, String(start), String(now), String(step)).catch((error) => {
          console.warn(error);
          return null;
        }),
      ])
        .then(([cpuResponse, memResponse, cpuRangeResponse, memRangeResponse]) => {
          if (cancelled) {
            return;
          }

          const map: Record<string, ServiceMetrics> = {};

          const ensure = (name: string) => {
            if (!map[name]) {
              map[name] = { ...emptyMetrics, cpuHistory: [], memoryHistory: [] };
            }

            return map[name];
          };

          parseInstant(cpuResponse, serviceLabel)?.forEach(([name, value]) => {
            ensure(name).cpu = value;
          });

          parseInstant(memResponse, serviceLabel)?.forEach(([name, value]) => {
            ensure(name).memory = value;
          });

          parseRange(cpuRangeResponse, serviceLabel)?.forEach(([name, values]) => {
            ensure(name).cpuHistory = values;
          });

          parseRange(memRangeResponse, serviceLabel)?.forEach(([name, values]) => {
            ensure(name).memoryHistory = values;
          });

          setByService(map);
        })
        .catch(console.warn);
    };

    doFetch();
    const interval = setInterval(doFetch, 30000);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [hasCadvisor]);

  const getForService = useCallback(
    (serviceName: string): ServiceMetrics => byService[serviceName] ?? emptyMetrics,
    [byService],
  );

  return { getForService, hasData: Object.keys(byService).length > 0 };
}
