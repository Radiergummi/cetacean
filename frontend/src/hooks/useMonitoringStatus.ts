import { useState, useEffect } from "react";
import { api } from "../api/client";
import type { MonitoringStatus } from "../api/types";

const TTL_MS = 60_000;

let cached: MonitoringStatus | null = null;
let cachedAt: number | null = null;
let inflight: Promise<void> | null = null;

export function _resetMonitoringStatusCache() {
  cached = null;
  cachedAt = null;
  inflight = null;
}

export function useMonitoringStatus(): MonitoringStatus | null {
  const [status, setStatus] = useState<MonitoringStatus | null>(cached);

  useEffect(() => {
    if (cached != null && cachedAt != null && Date.now() - cachedAt < TTL_MS) return;
    if (cached != null) {
      cached = null;
      inflight = null;
    }
    if (!inflight) {
      inflight = api
        .monitoringStatus()
        .then((s) => {
          cached = s;
          cachedAt = Date.now();
        })
        .catch(() => {
          inflight = null;
        });
    }
    inflight.then(() => {
      if (cached != null) setStatus(cached);
    });
  }, []);

  return status;
}
