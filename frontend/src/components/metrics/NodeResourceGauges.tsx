import { api } from "../../api/client";
import { escapePromQL } from "../../lib/utils";
import ResourceGauge from "./ResourceGauge";
import { useQuery } from "@tanstack/react-query";

interface GaugeDef {
  label: string;
  query: (instance?: string) => string;
}

function instanceFilter(instance?: string) {
  return instance ? `instance="${escapePromQL(instance)}"` : "";
}

const gauges: GaugeDef[] = [
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
      const filter = instanceFilter(instance);
      const selection = filter ? `{${filter}}` : "";

      return `(1 - node_memory_MemAvailable_bytes${selection} / node_memory_MemTotal_bytes${selection}) * 100`;
    },
  },
  {
    label: "Disk",
    query: (instance) => {
      const filter = instanceFilter(instance);

      return `max((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"${
        filter ? `,${filter}` : ""
      }} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs"${
        filter ? `,${filter}` : ""
      }}) * 100)`;
    },
  },
];

interface Props {
  /** Prometheus instance label (e.g. "10.100.9.27:9100"). Omit for cluster-wide. */
  instance?: string;
}

export default function NodeResourceGauges({ instance }: Props) {
  const { data: values = gauges.map(() => null) } = useQuery({
    queryKey: ["node-resource-gauges", instance],
    queryFn: () =>
      Promise.all(
        gauges.map((gauge) =>
          api
            .metricsQuery(gauge.query(instance))
            .then((response) => {
              const value = response.data?.result?.[0]?.value?.[1];

              return value != null ? Number(value) : null;
            })
            .catch(() => null),
        ),
      ),
    refetchInterval: 30_000,
    refetchIntervalInBackground: true,
    staleTime: 30_000,
    retry: false,
  });

  return (
    <div className="flex flex-wrap items-center justify-center gap-8 py-2">
      {gauges.map(({ label }, index) => (
        <ResourceGauge
          key={label}
          label={label}
          value={values[index]}
        />
      ))}
    </div>
  );
}
