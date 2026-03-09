import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import ResourceGauge from "./ResourceGauge";

interface GaugeDef {
  label: string;
  query: (addr?: string) => string;
}

function instanceFilter(addr?: string) {
  return addr ? `instance=~"${addr}:.*"` : "";
}

const GAUGES: GaugeDef[] = [
  {
    label: "CPU",
    query: (addr) => {
      const f = instanceFilter(addr);
      return `100 - (avg(rate(node_cpu_seconds_total{mode="idle"${f ? `,${f}` : ""}}[5m])) * 100)`;
    },
  },
  {
    label: "Memory",
    query: (addr) => {
      const f = instanceFilter(addr);
      const sel = f ? `{${f}}` : "";
      return `(1 - node_memory_MemAvailable_bytes${sel} / node_memory_MemTotal_bytes${sel}) * 100`;
    },
  },
  {
    label: "Disk",
    query: (addr) => {
      const f = instanceFilter(addr);
      return `max((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"${f ? `,${f}` : ""}} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"${f ? `,${f}` : ""}}) * 100)`;
    },
  },
];

export default function NodeResourceGauges({ addr }: { addr?: string }) {
  const [values, setValues] = useState<(number | null)[]>(GAUGES.map(() => null));

  const fetchAll = useCallback(() => {
    GAUGES.forEach((g, i) => {
      api
        .metricsQuery(g.query(addr))
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
  }, [addr]);

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
