import type { PrometheusResponse } from "@/api/types";
import SimpleTable from "@/components/SimpleTable";

interface Props {
  data: PrometheusResponse["data"];
}

interface NormalizedRow {
  metric: Record<string, string>;
  value: string;
  timestamp: number;
}

function normalizeRows(data: Props["data"]): NormalizedRow[] {
  return data.result
    .map(({ metric, value, values }) => {
      const point = value ?? values?.[values.length - 1];

      if (!point) {
        return null;
      }

      return { metric, value: point[1], timestamp: point[0] };
    })
    .filter((row): row is NormalizedRow => row !== null);
}

function LabelBadges({ metric }: { metric: Record<string, string> }) {
  const entries = Object.entries(metric).filter(([key]) => key !== "__name__");

  if (entries.length === 0) {
    return <span className="text-xs text-muted-foreground">—</span>;
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
  const rows = normalizeRows(data);

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
          <td className="p-3 font-mono text-sm">{row.metric["__name__"] ?? "—"}</td>
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
