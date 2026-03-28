import { useMonitoringStatus } from "./useMonitoringStatus";
import { api } from "@/api/client";
import type { ServiceSizing } from "@/api/types";
import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";

interface SizingHints {
  byServiceId: Map<string, ServiceSizing>;
  hasData: boolean;
}

const emptySizingHints: SizingHints = {
  byServiceId: new Map(),
  hasData: false,
};

export function useSizingHints(): SizingHints {
  const monitoring = useMonitoringStatus();
  const enabled = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
  const { pathname } = useLocation();

  const [hints, setHints] = useState<SizingHints>(emptySizingHints);

  useEffect(() => {
    if (!enabled) {
      setHints(emptySizingHints);
      return;
    }

    let cancelled = false;

    api
      .serviceSizing()
      .then((results) => {
        if (cancelled) {
          return;
        }

        const byServiceId = new Map<string, ServiceSizing>();

        for (const sizing of results) {
          byServiceId.set(sizing.serviceId, sizing);
        }

        setHints({
          byServiceId,
          hasData: true,
        });
      })
      .catch(() => {
        // Sizing hints are non-critical — fail silently
      });

    return () => {
      cancelled = true;
    };
  }, [enabled, pathname]);

  return hints;
}
