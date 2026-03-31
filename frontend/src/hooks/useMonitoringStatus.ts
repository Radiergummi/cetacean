import { api } from "../api/client";
import type { MonitoringStatus } from "../api/types";
import { useEffect, useRef, useState } from "react";

const ttl = 60_000;

interface StatusCache {
  value: MonitoringStatus | null;
  fetchedAt: number | null;
  inflight: Promise<MonitoringStatus | null> | null;
}

const cache: StatusCache = {
  value: null,
  fetchedAt: null,
  inflight: null,
};

export function _resetMonitoringStatusCache() {
  cache.value = null;
  cache.fetchedAt = null;
  cache.inflight = null;
}

export function useMonitoringStatus(): MonitoringStatus | null {
  const [status, setStatus] = useState<MonitoringStatus | null>(cache.value);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    if (cache.value != null && cache.fetchedAt != null && Date.now() - cache.fetchedAt < ttl) {
      setStatus(cache.value);

      return;
    }

    if (!cache.inflight) {
      cache.inflight = api
        .monitoringStatus()
        .then((result) => {
          cache.inflight = null;

          return result;
        })
        .catch((error) => {
          console.warn("monitoring status fetch failed:", error);
          cache.inflight = null;

          return null;
        });
    }

    cache.inflight.then((result) => {
      if (!mountedRef.current) {
        return;
      }

      if (result != null) {
        cache.value = result;
        cache.fetchedAt = Date.now();

        setStatus(result);
      }
    });

    return () => {
      mountedRef.current = false;
    };
  }, []);

  return status;
}
