import { useState, useEffect } from "react";
import { api } from "../api/client";
import type { MonitoringStatus } from "../api/types";

const TTL_MS = 60_000;

let cached: MonitoringStatus | null = null;
let cachedAt: number | null = null;
let inflight: Promise<MonitoringStatus> | null = null;

export function _resetMonitoringStatusCache() {
  cached = null;
  cachedAt = null;
  inflight = null;
}

export function useMonitoringStatus(): MonitoringStatus | null {
  const [status, setStatus] = useState<MonitoringStatus | null>(cached);

  useEffect(() => {
    let cancelled = false;

    if (cached != null && cachedAt != null && Date.now() - cachedAt < TTL_MS) {
      if (status !== cached) setStatus(cached);
      return;
    }

    if (!inflight) {
      inflight = api.monitoringStatus().catch(() => {
        inflight = null;
        return null as unknown as MonitoringStatus;
      });
    }

    inflight.then((s) => {
      if (cancelled) return;
      if (s != null) {
        cached = s;
        cachedAt = Date.now();
        setStatus(s);
      }
    });

    return () => {
      cancelled = true;
    };
  }, []);

  return status;
}
