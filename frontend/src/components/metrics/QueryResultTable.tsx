import type { PrometheusResponse } from "@/api/types";
import SimpleTable from "@/components/SimpleTable";
import { normalizePrometheusRows } from "@/lib/metricsParser";

interface Props {
  data: PrometheusResponse["data"];
}

function LabelBadges({ metric }: { metric: Record<string, string> }) {
  const entries = Object.entries(metric).filter(([key]) => key !== "__name__");

  if (entries.length === 0) {
    return <span className="text-xs text-muted-foreground">{"\u2014"}</span>;
  }

  return (
    <ul className="flex flex-wrap gap-1">
      {entries.map(([key, value]) => (
        <li
          key={key}
          className="inline-flex items-baseline overflow-hidden rounded-md border font-mono text-xs"
        >
          <span className="bg-muted/50 px-1.5 py-0.5 whitespace-nowrap text-muted-foreground">
            {key}
          </span>
          <span className="px-1.5 py-0.5 whitespace-nowrap">{value}</span>
        </li>
      ))}
    </ul>
  );
}

/**
 * Renders a Prometheus query result as a table.
 *
 * For vector results, each row is one series.
 * For matrix results, each row shows the latest value per series.
 * Labels are rendered as key=value badges, excluding __name__.
 */
export default function QueryResultTable({ data }: Props) {
  const rows = normalizePrometheusRows(data);

  if (rows.length === 0) {
    return <p className="py-4 text-center text-sm text-muted-foreground">No results</p>;
  }

  return (
    <SimpleTable
      columns={["Metric", "Labels", "Value", "Timestamp"]}
      items={rows}
      keyFn={(_, index) => index}
      renderRow={(row) => (
        <>
          <td className="p-3 font-mono text-sm">{row.metric["__name__"] ?? "\u2014"}</td>
          <td className="p-3">
            <LabelBadges metric={row.metric} />
          </td>
          <td className="p-3 font-mono text-sm">{row.value}</td>
          <td className="p-3 text-sm text-muted-foreground">
            {new Date(row.timestamp * 1000).toLocaleString()}
          </td>
        </>
      )}
    />
  );
}
