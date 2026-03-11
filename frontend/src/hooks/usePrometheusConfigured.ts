import { useState, useEffect } from "react";
import { api } from "../api/client";

// Caches the result at module level since this is a static server config value.
let cached: boolean | null = null;
let inflight: Promise<void> | null = null;

// Reset for testing — clears the module-level cache.
export function _resetPrometheusCache() {
  cached = null;
  inflight = null;
}

export function usePrometheusConfigured(): boolean | null {
  const [configured, setConfigured] = useState<boolean | null>(cached);

  useEffect(() => {
    if (cached != null) return;
    if (!inflight) {
      inflight = api
        .cluster()
        .then((s) => {
          cached = s.prometheusConfigured;
        })
        .catch(() => {
          cached = false;
        });
    }
    inflight.then(() => setConfigured(cached));
  }, []);

  return configured;
}
