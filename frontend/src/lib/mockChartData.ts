import { getChartColor } from "./chartColors";

/**
 * Generate mock time-series data for local development when Prometheus has no matching data.
 * Only used when import.meta.env.DEV is true.
 */
export function generateMockSeries(
  title: string,
  unit: string | undefined,
  start: number,
  end: number,
  step: number,
  colorOverride?: string,
): {
  labels: string[];
  timestamps: number[];
  series: { label: string; color: string; data: number[] }[];
} {
  const count = Math.floor((end - start) / step);
  const timestamps: number[] = [];

  for (let i = 0; i < count; i++) {
    timestamps.push(start + i * step);
  }

  const labels = timestamps.map((timestamp) => new Date(timestamp * 1000).toLocaleTimeString());
  const isBytes = unit === "bytes" || unit === "bytes/s";
  const isPercentage = unit === "%";
  const seriesCount = title.toLowerCase().includes("top") ? 6 : 2;

  const mockNames = [
    "webapp-production",
    "webapp-staging",
    "control-plane",
    "monitoring",
    "agent-playground",
    "data-pipeline",
    "auth-service",
    "api-gateway",
    "cache-layer",
    "worker-pool",
  ];

  const series = Array.from({ length: seriesCount }, (_, index) => {
    const base = isPercentage
      ? 10 + Math.random() * 40
      : isBytes
        ? 1e8 + Math.random() * 2e9
        : Math.random() * 100;
    const volatility = base * 0.15;
    let val = base;
    const data = timestamps.map(() => {
      val += (Math.random() - 0.48) * volatility;
      val = Math.max(0, val);
      return val;
    });

    return {
      label: mockNames[index % mockNames.length],
      color: colorOverride ?? getChartColor(index),
      data,
    };
  });

  return { labels, timestamps, series };
}
