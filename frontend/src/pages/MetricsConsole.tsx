import { api } from "@/api/client";
import type { PrometheusResponse } from "@/api/types";
import ErrorBoundary from "@/components/ErrorBoundary";
import MonitoringStatus from "@/components/metrics/MonitoringStatus";
import { QueryInput } from "@/components/metrics/QueryInput";
import QueryResultTable from "@/components/metrics/QueryResultTable";
import TimeSeriesChart from "@/components/metrics/TimeSeriesChart";
import { useQueryCompletion } from "@/components/metrics/useQueryCompletion";
import PageHeader from "@/components/PageHeader";
import type { Segment } from "@/components/SegmentedControl";
import SegmentedControl from "@/components/SegmentedControl";
import { useMonitoringStatus } from "@/hooks/useMonitoringStatus";
import { getErrorMessage } from "@/lib/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";
import { useSearchParams } from "react-router-dom";

const ranges = ["1h", "6h", "24h", "7d"] as const;
const rangeSegments: Segment<string>[] = ranges.map((value) => ({
  label: value.toUpperCase(),
  value,
}));

export default function MetricsConsole() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [input, setInput] = useState(searchParams.get("q") ?? "");
  const range = searchParams.get("range") ?? "1h";
  const [refreshKey, setRefreshKey] = useState(0);
  const [activeQuery, setActiveQuery] = useState<string | null>(
    searchParams.get("q")?.trim() || null,
  );
  const monitoring = useMonitoringStatus();
  const completion = useQueryCompletion(!!monitoring?.prometheusReachable);
  const queryClient = useQueryClient();

  const {
    data: result,
    isLoading: loading,
    error: queryError,
  } = useQuery<PrometheusResponse["data"]>({
    queryKey: ["metrics-instant", activeQuery],
    queryFn: () => api.metricsQuery(activeQuery!).then((r) => r.data),
    enabled: !!activeQuery,
    staleTime: 0,
  });

  const error = queryError ? getErrorMessage(queryError, "Query failed") : null;

  const runQuery = useCallback(() => {
    const query = input.trim();

    if (!query) {
      return;
    }

    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.set("q", query);
        return next;
      },
      { replace: true },
    );

    setActiveQuery(query);
    setRefreshKey((key) => key + 1);
    void queryClient.invalidateQueries({ queryKey: ["metrics-instant", query] });
  }, [input, setSearchParams, queryClient]);

  const setRange = (value: string) => {
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);

        if (value === "1h") {
          next.delete("range");
        } else {
          next.set("range", value);
        }

        return next;
      },
      { replace: true },
    );
  };

  return (
    <div>
      <PageHeader title="Query Console" />

      {monitoring && <MonitoringStatus status={monitoring} />}

      <div className="space-y-4">
        <QueryInput
          value={input}
          onChange={setInput}
          onRun={runQuery}
          loading={loading}
          completion={completion}
        />

        {error && (
          <div className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
            {error}
          </div>
        )}

        {activeQuery && (
          <>
            <div className="flex items-center gap-2">
              <SegmentedControl
                segments={rangeSegments}
                value={range}
                onChange={setRange}
              />
            </div>

            <ErrorBoundary inline>
              <TimeSeriesChart
                title={activeQuery.length > 60 ? activeQuery.slice(0, 57) + "..." : activeQuery}
                query={activeQuery}
                range={range}
                refreshKey={refreshKey}
              />
            </ErrorBoundary>
          </>
        )}

        {result && <QueryResultTable data={result} />}
      </div>
    </div>
  );
}
