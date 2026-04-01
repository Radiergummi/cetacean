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

function seriesLabel(metric: Record<string, string>): string {
  const name = metric["__name__"] ?? "";
  const labels = Object.entries(metric)
    .filter(([key]) => key !== "__name__")
    .map(([key, value]) => `${key}="${value}"`)
    .join(", ");

  if (!name && !labels) {
    return "{}";
  }

  if (!labels) {
    return name;
  }

  return `${name}{${labels}}`;
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

/**
 * Renders all raw data points from a matrix result, like the Prometheus UI table tab.
 * Each series is a collapsible group showing every [timestamp, value] pair.
 */
export function MatrixResultTable({ data }: Props) {
  if (data.resultType !== "matrix" || data.result.length === 0) {
    return <p className="py-4 text-center text-sm text-muted-foreground">No results</p>;
  }

  return (
    <div className="space-y-4">
      {data.result.map(({ metric, values }, seriesIndex) => {
        const label = seriesLabel(metric);
        const points = values ?? [];

        return (
          <div
            key={seriesIndex}
            className="overflow-hidden rounded-lg border"
          >
            <div className="border-b bg-muted/50 px-3 py-2">
              <span className="font-mono text-sm font-medium">{label}</span>
              <span className="ml-2 text-xs text-muted-foreground">
                {points.length} {points.length === 1 ? "sample" : "samples"}
              </span>
            </div>

            <div className="max-h-64 overflow-auto">
              <table className="w-full">
                <thead className="sticky top-0 bg-background text-xs text-muted-foreground">
                  <tr>
                    <th className="px-3 py-1.5 text-left font-medium">Timestamp</th>
                    <th className="px-3 py-1.5 text-right font-medium">Value</th>
                  </tr>
                </thead>
                <tbody className="text-sm">
                  {points.map(([timestamp, value], pointIndex) => (
                    <tr
                      key={pointIndex}
                      className="border-t border-border/50"
                    >
                      <td className="px-3 py-1 text-muted-foreground">
                        {new Date(timestamp * 1000).toLocaleString()}
                      </td>
                      <td className="px-3 py-1 text-right font-mono">{value}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        );
      })}
    </div>
  );
}
