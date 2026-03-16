import { api } from "../../api/client";
import { escapePromQL } from "../../lib/utils";
import ResourceGauge from "./ResourceGauge";
import { useState, useEffect, useCallback } from "react";

interface GaugeDef {
  label: string;
  query: (instance?: string) => string;
}

function instanceFilter(instance?: string) {
  return instance ? `instance="${escapePromQL(instance)}"` : "";
}

const GAUGES: GaugeDef[] = [
  {
    label: "CPU",
    query: (instance) => {
      const f = instanceFilter(instance);
      return `100 - (avg(rate(node_cpu_seconds_total{mode="idle"${f ? `,${f}` : ""}}[5m])) * 100)`;
    },
  },
  {
    label: "Memory",
    query: (instance) => {
      const f = instanceFilter(instance);
      const sel = f ? `{${f}}` : "";
      return `(1 - node_memory_MemAvailable_bytes${sel} / node_memory_MemTotal_bytes${sel}) * 100`;
    },
  },
  {
    label: "Disk",
    query: (instance) => {
      const f = instanceFilter(instance);
      return `max((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"${f ? `,${f}` : ""}} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"${f ? `,${f}` : ""}}) * 100)`;
    },
  },
];

interface Props {
  /** Prometheus instance label (e.g. "10.100.9.27:9100"). Omit for cluster-wide. */
  instance?: string;
}

export default function NodeResourceGauges({ instance }: Props) {
  const [values, setValues] = useState<(number | null)[]>(GAUGES.map(() => null));

  const fetchAll = useCallback(() => {
    Promise.all(
      GAUGES.map((g) =>
        api
          .metricsQuery(g.query(instance))
          .then((resp) => {
            const val = resp.data?.result?.[0]?.value?.[1];
            return val != null ? Number(val) : null;
          })
          .catch(() => null),
      ),
    ).then(setValues);
  }, [instance]);

  useEffect(() => {
    fetchAll();
    const interval = setInterval(fetchAll, 30000);
    return () => clearInterval(interval);
  }, [fetchAll]);

  return (
    <div className="flex items-center justify-center gap-8 py-2">
      {GAUGES.map((g, i) => (
        <ResourceGauge
          key={g.label}
          label={g.label}
          value={values[i]}
        />
      ))}
    </div>
  );
}
