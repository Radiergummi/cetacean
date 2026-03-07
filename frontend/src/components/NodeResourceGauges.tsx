import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import ResourceGauge from "./ResourceGauge";

interface GaugeDef {
  label: string;
  query: string;
  formatSubtitle?: (raw: Record<string, any>) => string;
}

const GAUGES: GaugeDef[] = [
  {
    label: "CPU",
    query: `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`,
  },
  {
    label: "Memory",
    query: `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`,
  },
  {
    label: "Disk",
    query: `max((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"}) * 100)`,
  },
];

export default function NodeResourceGauges() {
  const [values, setValues] = useState<(number | null)[]>(GAUGES.map(() => null));

  const fetchAll = useCallback(() => {
    GAUGES.forEach((g, i) => {
      api
        .metricsQuery(g.query)
        .then((resp: any) => {
          const val = resp.data?.result?.[0]?.value?.[1];
          setValues((prev) => {
            const next = [...prev];
            next[i] = val != null ? Number(val) : null;
            return next;
          });
        })
        .catch(() => {});
    });
  }, []);

  useEffect(() => {
    fetchAll();
    const interval = setInterval(fetchAll, 30000);
    return () => clearInterval(interval);
  }, [fetchAll]);

  return (
    <div className="flex items-center justify-center gap-8 py-2">
      {GAUGES.map((g, i) => (
        <ResourceGauge key={g.label} label={g.label} value={values[i]} />
      ))}
    </div>
  );
}
