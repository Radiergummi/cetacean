import { useState, useEffect } from "react";
import { api } from "../api/client";

// Caches the result at module level since this is a static server config value.
let cached: boolean | null = null;

export function usePrometheusConfigured(): boolean | null {
  const [configured, setConfigured] = useState<boolean | null>(cached);

  useEffect(() => {
    if (cached != null) return;
    api.cluster().then((s) => {
      cached = s.prometheusConfigured;
      setConfigured(cached);
    }).catch(() => {
      // If cluster fetch fails, assume not configured to avoid broken metrics UI.
      cached = false;
      setConfigured(false);
    });
  }, []);

  return configured;
}
