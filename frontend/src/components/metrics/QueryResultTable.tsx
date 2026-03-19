import SimpleTable from "@/components/SimpleTable";

interface VectorResult {
  metric: Record<string, string>;
  value: [number, string];
}

interface MatrixResult {
  metric: Record<string, string>;
  values: [number, string][];
}

interface Props {
  data: {
    resultType: "vector" | "matrix" | "scalar" | "string";
    result: VectorResult[] | MatrixResult[];
  };
}

interface NormalizedRow {
  metric: Record<string, string>;
  value: string;
  timestamp: number;
}

function normalizeRows(data: Props["data"]): NormalizedRow[] {
  if (data.resultType === "vector") {
    return (data.result as VectorResult[]).map(({ metric, value }) => ({
      metric,
      value: value[1],
      timestamp: value[0],
    }));
  }

  if (data.resultType === "matrix") {
    return (data.result as MatrixResult[]).map(({ metric, values }) => {
      const last = values[values.length - 1] ?? [0, ""];
      return {
        metric,
        value: last[1],
        timestamp: last[0],
      };
    });
  }

  return [];
}

function LabelBadges({ metric }: { metric: Record<string, string> }) {
  const entries = Object.entries(metric).filter(([key]) => key !== "__name__");

  if (entries.length === 0) {
    return <span className="text-muted-foreground text-xs">—</span>;
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
    return (
      <p className="text-sm text-muted-foreground py-4 text-center">
        No results
      </p>
    );
  }

  return (
    <SimpleTable
      columns={["Metric", "Labels", "Value", "Timestamp"]}
      items={rows}
      keyFn={(row, index) => `${JSON.stringify(row.metric)}-${index}`}
      renderRow={(row) => (
        <>
          <td className="p-3 text-sm font-mono">
            {row.metric["__name__"] ?? "—"}
          </td>
          <td className="p-3">
            <LabelBadges metric={row.metric} />
          </td>
          <td className="p-3 text-sm font-mono">{row.value}</td>
          <td className="p-3 text-sm text-muted-foreground">
            {new Date(row.timestamp * 1000).toLocaleString()}
          </td>
        </>
      )}
    />
  );
}
