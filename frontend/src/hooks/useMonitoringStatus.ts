import { useState, useEffect } from "react";
import { api } from "../api/client";
import type { MonitoringStatus } from "../api/types";

let cached: MonitoringStatus | null = null;
let inflight: Promise<void> | null = null;

export function _resetMonitoringStatusCache() {
  cached = null;
  inflight = null;
}

export function useMonitoringStatus(): MonitoringStatus | null {
  const [status, setStatus] = useState<MonitoringStatus | null>(cached);

  useEffect(() => {
    if (cached != null) return;
    if (!inflight) {
      inflight = api
        .monitoringStatus()
        .then((s) => {
          cached = s;
        })
        .catch(() => {
          cached = {
            prometheusConfigured: false,
            prometheusReachable: false,
            nodeExporter: null,
            cadvisor: null,
          };
        });
    }
    inflight.then(() => setStatus(cached));
  }, []);

  return status;
}
