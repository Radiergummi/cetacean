import TimeSeriesChart from "./TimeSeriesChart";
import type { PrometheusResponse } from "@/api/types.ts";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ChartData, ChartOptions, Plugin } from "chart.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * Characterisation tests. This chart had none, which is why the review left it
 * alone: 934 lines of prop-mirroring refs and imperative Chart.js plugins is
 * not something to restructure on faith. These pin what it does today — the
 * dataset it hands Chart.js in each mode, the states it renders, when it opens
 * a stream and what an appended point does — so the decomposition that follows
 * has something to fail against.
 *
 * jsdom has no canvas, so `Line` is replaced with a recorder. That is not a
 * limitation here: `data`, `options` and `plugins` are the entire contract
 * between this component and Chart.js, and asserting on them is stricter than
 * asserting on pixels.
 */

interface LineProps {
  data: ChartData<"line">;
  options: ChartOptions<"line">;
  plugins: Plugin<"line">[];
}

const lineProps: LineProps[] = [];

vi.mock("react-chartjs-2", () => ({
  Line: (props: LineProps) => {
    lineProps.push(props);

    return <div data-testid="chart-canvas" />;
  },
}));

const metricsQueryRange = vi.fn<(...args: string[]) => Promise<PrometheusResponse>>();
const metricsStreamURL = vi.fn<(query: string, step: number, range: number) => string>(
  () => "/metrics/stream",
);

vi.mock("@/api/client.ts", () => ({
  api: {
    metricsQueryRange: (...args: string[]) => metricsQueryRange(...args),
    metricsStreamURL: (query: string, step: number, range: number) =>
      metricsStreamURL(query, step, range),
  },
}));

const streamListeners: Record<string, (event: MessageEvent) => void> = {};
const closeStream = vi.fn<() => void>();

vi.mock("@/lib/eventStream.ts", () => ({
  openEventStream: (
    _url: string,
    { listeners }: { listeners: Record<string, (event: MessageEvent) => void> },
  ) => {
    Object.assign(streamListeners, listeners);

    return { close: closeStream };
  },
}));

/** Two series, three points each, at ten-second spacing. */
function response(): PrometheusResponse {
  return {
    status: "success",
    data: {
      resultType: "matrix",
      result: [
        {
          metric: { instance: "alpha" },
          values: [
            [1000, "1"],
            [1010, "2"],
            [1020, "3"],
          ],
        },
        {
          metric: { instance: "beta" },
          values: [
            [1000, "4"],
            [1010, "5"],
            [1020, "6"],
          ],
        },
      ],
    },
  } as unknown as PrometheusResponse;
}

/** The props every test shares. */
const base = {
  title: "CPU",
  query: "rate(cpu[1m])",
  range: "1h",
};

/** The most recent set of props handed to Chart.js. */
function latestChart(): LineProps {
  const last = lineProps.at(-1);

  if (!last) {
    throw new Error("Chart.js was never rendered");
  }

  return last;
}

async function renderChart(props: Partial<Parameters<typeof TimeSeriesChart>[0]> = {}) {
  const result = render(
    <TimeSeriesChart
      {...base}
      {...props}
    />,
  );

  await screen.findByTestId("chart-canvas");

  return result;
}

beforeEach(() => {
  lineProps.length = 0;
  metricsQueryRange.mockReset();
  metricsQueryRange.mockResolvedValue(response());
  metricsStreamURL.mockClear();
  closeStream.mockClear();

  for (const key of Object.keys(streamListeners)) {
    delete streamListeners[key];
  }
});

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("TimeSeriesChart data states", () => {
  it("queries the range Prometheus is asked for and then draws", async () => {
    await renderChart();

    expect(metricsQueryRange).toHaveBeenCalledTimes(1);

    const [query, start, end, step] = metricsQueryRange.mock.calls[0] as string[];

    expect(query).toBe(base.query);
    expect(Number(end) - Number(start)).toBe(3600);
    expect(Number(step)).toBe(15);
  });

  it("shows an unknown range as one hour", async () => {
    await renderChart({ range: "not-a-range" });

    const [, start, end] = metricsQueryRange.mock.calls[0] as string[];

    expect(Number(end) - Number(start)).toBe(3600);
  });

  it("reports a failed query and refetches when retried", async () => {
    metricsQueryRange.mockRejectedValueOnce(new Error("Prometheus is unreachable"));

    render(<TimeSeriesChart {...base} />);

    await screen.findByText("Prometheus is unreachable");

    metricsQueryRange.mockResolvedValue(response());
    await userEvent.click(screen.getByRole("button", { name: /retry/i }));

    await screen.findByTestId("chart-canvas");
    expect(metricsQueryRange).toHaveBeenCalledTimes(2);
  });

  it("says so when the range holds no data", async () => {
    vi.stubEnv("DEV", false);
    metricsQueryRange.mockResolvedValue({
      status: "success",
      data: { resultType: "matrix", result: [] },
    } as unknown as PrometheusResponse);

    render(<TimeSeriesChart {...base} />);

    expect(await screen.findByText("No data for this time range")).toBeInTheDocument();
    expect(screen.queryByTestId("chart-canvas")).not.toBeInTheDocument();
  });

  it("hands the series back to the caller as it parsed them", async () => {
    const onSeriesInfo = vi.fn<(series: { label: string; color: string }[]) => void>();

    await renderChart({ onSeriesInfo });

    expect(onSeriesInfo).toHaveBeenCalledWith([
      { label: "alpha", color: expect.any(String) },
      { label: "beta", color: expect.any(String) },
    ]);
  });
});

describe("TimeSeriesChart datasets", () => {
  it("draws one filled line per series", async () => {
    await renderChart();

    const { data } = latestChart();

    expect(data.labels).toHaveLength(3);
    expect(data.datasets).toHaveLength(2);
    expect(data.datasets[0]?.label).toBe("alpha");
    expect(data.datasets[0]?.data).toEqual([1, 2, 3]);
    expect(data.datasets[0]?.fill).toBe(true);
    expect(data.datasets[0]?.borderWidth).toBe(1.5);
  });

  it("renames series for display without changing their data", async () => {
    await renderChart({ labelTransform: (label: string) => label.toUpperCase() });

    const { data } = latestChart();

    expect(data.datasets[0]?.label).toBe("ALPHA");
    expect(data.datasets[0]?.data).toEqual([1, 2, 3]);
  });

  it("stacks into areas when the stacked toggle is used", async () => {
    await renderChart({ stackable: true });

    await userEvent.click(screen.getByTitle("Stacked area"));

    const { data, options } = latestChart();

    expect(data.datasets[0]?.fill).toBe("stack");
    expect(data.datasets[0]?.borderWidth).toBe(1);
    expect(options.scales?.["y"]?.stacked).toBe(true);
  });

  it("offers no stacking toggle unless asked for one", async () => {
    await renderChart();

    expect(screen.queryByTitle("Stacked area")).not.toBeInTheDocument();
  });

  it("dims every series but the isolated one", async () => {
    await renderChart({ isolatedLabel: "beta" });

    const { data } = latestChart();

    expect(data.datasets[0]?.fill).toBe(false);
    expect(data.datasets[0]?.borderColor).toMatch(/4D$/);
    expect(data.datasets[1]?.fill).toBe(true);
  });

  it("zeroes a dimmed series when stacked, so it takes no height", async () => {
    await renderChart({ isolatedLabel: "beta", stackable: true });

    await userEvent.click(screen.getByTitle("Stacked area"));

    const { data } = latestChart();

    expect(data.datasets[0]?.data).toEqual([0, 0, 0]);
    expect(data.datasets[1]?.data).toEqual([4, 5, 6]);
  });

  it("ignores an isolation naming a series that is not drawn", async () => {
    await renderChart({ isolatedLabel: "gamma" });

    const { data } = latestChart();

    expect(data.datasets[0]?.fill).toBe(true);
    expect(data.datasets[1]?.fill).toBe(true);
  });
});

describe("TimeSeriesChart axis", () => {
  it("leaves the axis to Chart.js when nothing constrains it", async () => {
    await renderChart();

    expect(latestChart().options.scales?.["y"]?.suggestedMax).toBeUndefined();
  });

  it("raises the axis above a threshold sitting over the data", async () => {
    await renderChart({
      yMin: 0,
      thresholds: [{ label: "limit", value: 100, color: "#f00" }],
    });

    // 100 is the high, 0 the low, and the axis takes a tenth of that as headroom.
    expect(latestChart().options.scales?.["y"]?.suggestedMax).toBeCloseTo(110);
  });

  it("passes a floor through to the axis", async () => {
    await renderChart({ yMin: 0 });

    expect(latestChart().options.scales?.["y"]?.min).toBe(0);
  });
});

describe("TimeSeriesChart live stream", () => {
  it("opens a stream for a relative range once the first fetch lands", async () => {
    await renderChart();

    await waitFor(() => expect(metricsStreamURL).toHaveBeenCalled());

    const [query, step, rangeSeconds] = metricsStreamURL.mock.calls[0] as unknown[];

    expect(query).toBe(base.query);
    expect(step).toBe(15);
    expect(rangeSeconds).toBe(3600);
  });

  it("opens no stream for an explicit window", async () => {
    await renderChart({ from: 1000, to: 2000 });

    expect(metricsStreamURL).not.toHaveBeenCalled();
  });

  it("appends a streamed point to the series it belongs to", async () => {
    await renderChart();

    await waitFor(() => expect(streamListeners["point"]).toBeDefined());

    streamListeners["point"]?.({
      data: JSON.stringify({
        status: "success",
        data: {
          resultType: "vector",
          result: [
            { metric: { instance: "alpha" }, value: [1030, "7"] },
            { metric: { instance: "beta" }, value: [1030, "8"] },
          ],
        },
      }),
    } as MessageEvent);

    await waitFor(() => expect(latestChart().data.datasets[0]?.data).toEqual([2, 3, 7]));
    expect(latestChart().data.datasets[1]?.data).toEqual([5, 6, 8]);
  });

  it("survives a malformed frame rather than tearing the chart down", async () => {
    await renderChart();

    await waitFor(() => expect(streamListeners["point"]).toBeDefined());

    streamListeners["point"]?.({ data: "not json" } as MessageEvent);

    expect(latestChart().data.datasets[0]?.data).toEqual([1, 2, 3]);
  });
});

describe("TimeSeriesChart chrome", () => {
  it("names the chart and its unit", async () => {
    await renderChart({ unit: "%" });

    expect(screen.getByText("CPU")).toBeInTheDocument();
    expect(screen.getByText("%")).toBeInTheDocument();
  });

  it("carries both the threshold and the crosshair plugin", async () => {
    await renderChart();

    expect(latestChart().plugins.map(({ id }) => id)).toEqual(["thresholdLines", "crosshair"]);
  });
});
