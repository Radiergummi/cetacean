import { api } from "@/api/client";
import type { PrometheusResponse } from "@/api/types";
import EmptyState from "@/components/EmptyState";
import ErrorBoundary from "@/components/ErrorBoundary";
import { rangeSegments } from "@/components/metrics/MetricsPanelContext";
import MonitoringStatus from "@/components/metrics/MonitoringStatus";
import { QueryInput } from "@/components/metrics/QueryInput";
import QueryResultTable from "@/components/metrics/QueryResultTable";
import TimeSeriesChart from "@/components/metrics/TimeSeriesChart";
import { useQueryCompletion } from "@/components/metrics/useQueryCompletion";
import PageHeader from "@/components/PageHeader";
import SegmentedControl from "@/components/SegmentedControl";
import { useMonitoringStatus } from "@/hooks/useMonitoringStatus";
import { useUrlParam } from "@/hooks/useUrlParam";
import { getErrorMessage } from "@/lib/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";

export default function MetricsConsole() {
  const [urlQuery, setUrlQuery] = useUrlParam("q");
  const [range, setRange] = useUrlParam("range", "1h");
  const [input, setInput] = useState(urlQuery ?? "");
  const [refreshKey, setRefreshKey] = useState(0);
  const [activeQuery, setActiveQuery] = useState<string | null>(urlQuery?.trim() || null);
  const monitoring = useMonitoringStatus();
  const queryable = monitoring?.queryable === true;
  const completion = useQueryCompletion(!!monitoring?.prometheusReachable && queryable);
  const queryClient = useQueryClient();

  const {
    data: result,
    isLoading: loading,
    error: queryError,
  } = useQuery<PrometheusResponse["data"]>({
    queryKey: ["metrics-instant", activeQuery],
    queryFn: () => api.metricsQuery(activeQuery!).then((r) => r.data),
    enabled: !!activeQuery && queryable,
    staleTime: 0,
  });

  const error = queryError ? getErrorMessage(queryError, "Query failed") : null;

  const runQuery = useCallback(() => {
    const query = input.trim();

    if (!query) {
      return;
    }

    setUrlQuery(query);
    setActiveQuery(query);
    setRefreshKey((key) => key + 1);

    // Force refetch if the same query is re-run (key doesn't change).
    void queryClient.invalidateQueries({ queryKey: ["metrics-instant", query] });
  }, [input, setUrlQuery, queryClient]);

  if (monitoring && !queryable) {
    return (
      <div>
        <PageHeader title="Query Console" />
        <EmptyState message="Metrics aren't available for your access." />
      </div>
    );
  }

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

        {activeQuery && queryable && (
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
