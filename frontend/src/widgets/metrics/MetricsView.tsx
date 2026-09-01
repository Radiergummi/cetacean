import { MetricsChart } from "./MetricsChart";
import { type MetricsResult, metricRanges } from "./types";
import { getChartColor } from "@/lib/chartColors";
import { formatBytes, formatBytesPerSecond, formatNumber } from "@/lib/format";

interface Props {
  result: MetricsResult;
  onRangeChange: (range: string) => void;
}

/**
 * The presentational half of the metrics widget: header, range picker, legend
 * and chart.
 *
 * Every series is named in the legend and carries its latest value as text.
 * That is not decoration: the palette's blue sits below 3:1 against a light
 * surface, and a chart that leans on colour alone to say which line is which
 * fails the reader who cannot separate them.
 */
export function MetricsView({ onRangeChange, result }: Props) {
  const formatValue = valueFormatter(result.unit);
  const hasData = result.series.some(({ points }) => points.length > 0);

  return (
    <div className="flex h-full flex-col gap-3 p-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-sm font-medium">
          {result.name} <span className="opacity-60">· {result.metric}</span>
        </h1>

        <div
          role="group"
          aria-label="Time range"
          className="flex items-center overflow-hidden rounded-md border"
        >
          {metricRanges.map((range) => (
            <button
              key={range}
              type="button"
              onClick={() => onRangeChange(range)}
              aria-pressed={range === result.range}
              className="px-2 py-1 text-xs text-muted-foreground hover:bg-muted aria-pressed:bg-primary aria-pressed:text-primary-foreground"
            >
              {range}
            </button>
          ))}
        </div>
      </div>

      {hasData ? (
        <>
          <ul
            aria-label="Series"
            className="flex flex-wrap gap-x-4 gap-y-1"
          >
            {result.series.map((series, index) => (
              <li
                key={series.name}
                className="flex items-baseline gap-1.5 text-xs"
              >
                <span
                  aria-hidden="true"
                  className="size-2 shrink-0 rounded-full"
                  style={{ backgroundColor: getChartColor(index === 0 ? 0 : 2) }}
                />
                <span className="text-muted-foreground">{series.name}</span>
                <span className="font-medium tabular-nums">
                  {formatValue(latestValue(series.points))}
                </span>
              </li>
            ))}
          </ul>

          <div className="min-h-0 flex-1">
            <MetricsChart
              series={result.series}
              formatValue={formatValue}
            />
          </div>
        </>
      ) : (
        <p className="p-3 text-sm opacity-70">
          No data for {result.name} over the last {result.range}. Prometheus is reachable but has no
          samples for this metric — check that cAdvisor and node-exporter are scraping.
        </p>
      )}
    </div>
  );
}

function latestValue(points: { value: number }[]): number {
  return points[points.length - 1]?.value ?? 0;
}

/**
 * Renders a value in the unit the tool reported, reusing the dashboard's
 * formatters so a widget and a chart never disagree about what "2 kB/s" means.
 */
function valueFormatter(unit: string): (value: number) => string {
  switch (unit) {
    case "percent":
      return (value) => `${formatNumber(value, 1)}%`;

    case "bytes":
      return formatBytes;

    case "bytes/s":
      return formatBytesPerSecond;

    default:
      return (value) => formatNumber(value, 2);
  }
}
