import { api } from "../api/client";
import type { MonitoringStatus } from "../api/types";
import { useEffect, useState } from "react";

const ttl = 60_000;

let cached: MonitoringStatus | null = null;
let cachedAt: number | null = null;
let inflight: Promise<MonitoringStatus | null> | null = null;

export function _resetMonitoringStatusCache() {
  cached = null;
  cachedAt = null;
  inflight = null;
}

export function useMonitoringStatus(): MonitoringStatus | null {
  const [status, setStatus] = useState<MonitoringStatus | null>(cached);

  useEffect(() => {
    let cancelled = false;

    if (cached != null && cachedAt != null && Date.now() - cachedAt < ttl) {
      setStatus(cached);
      return;
    }

    if (!inflight) {
      inflight = api
        .monitoringStatus()
        .then((status) => {
          inflight = null;

          return status;
        })
        .catch((error) => {
          console.warn("monitoring status fetch failed:", error);
          inflight = null;

          return null;
        });
    }

    inflight.then((status) => {
      if (cancelled) {
        return;
      }

      if (status != null) {
        cached = status;
        cachedAt = Date.now();

        setStatus(status);
      }
    });

    return () => {
      cancelled = true;
    };
  }, []);

  return status;
}
