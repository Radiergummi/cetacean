import { api } from "../api/client";
import type { Node } from "../api/types";
import DataTable, { type Column } from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import { SkeletonTable } from "../components/LoadingSkeleton";
import { ResourceGauge, Sparkline, NodeResourceGauges, MetricsPanel } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import ResourceCard from "../components/ResourceCard";
import TaskStatusBadge from "../components/TaskStatusBadge";
import { isNodeExporterReady, useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useNodeMetrics } from "../hooks/useNodeMetrics";
import { useSearchParam } from "../hooks/useSearchParam";
import { useSortParams } from "../hooks/useSort";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { instanceToHostname } from "../lib/format";
import { sortColumn } from "../lib/sortColumn";
import { cardGridClass } from "../lib/styles";
import { useCallback, useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";

export default function NodeList() {
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("hostname");
  const {
    data: nodes,
    loading,
    error,
    retry,
    hasMore,
    loadMore,
  } = useSwarmResource(
    useCallback(
      (offset: number, signal: AbortSignal) =>
        api.nodes({ search: debouncedSearch, sort: sortKey, dir: sortDir, offset }, signal),
      [debouncedSearch, sortKey, sortDir],
    ),
    "node",
    ({ ID }: Node) => ID,
  );
  const [viewMode, setViewMode] = useViewMode("nodes");
  const navigate = useNavigate();
  const monitoring = useMonitoringStatus();
  const hasNodeExporter = isNodeExporterReady(monitoring);
  const { getForNode } = useNodeMetrics();

  const baseColumns: Column<Node>[] = useMemo(
    () => [
      {
        ...sortColumn("Hostname", "hostname", sortKey, sortDir, toggle),
        cell: ({ Description, ID }) => (
          <Link
            to={`/nodes/${ID}`}
            className="font-medium text-link hover:underline"
            onClick={(event) => event.stopPropagation()}
          >
            {Description.Hostname || ID}
          </Link>
        ),
      },
      {
        ...sortColumn("Role", "role", sortKey, sortDir, toggle),
        cell: ({ ManagerStatus, Spec }) => (
          <span className="flex items-center gap-1.5">
            {Spec.Role}
            {ManagerStatus?.Leader && (
              <span className="rounded bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
                Leader
              </span>
            )}
          </span>
        ),
      },
      {
        ...sortColumn("Availability", "availability", sortKey, sortDir, toggle),
        cell: ({ Spec }) => Spec.Availability,
      },
      {
        ...sortColumn("Status", "status", sortKey, sortDir, toggle),
        cell: ({ Status }) => <TaskStatusBadge state={Status.State} />,
      },
      {
        header: "Address",
        cell: ({ Status }) => <span className="tabular-nums">{Status.Addr}</span>,
      },
    ],
    [sortKey, sortDir, toggle],
  );

  const metricsColumns: Column<Node>[] = useMemo(
    () =>
      hasNodeExporter
        ? [
            {
              header: "CPU",
              cell: ({ Description, Status }) => {
                const metrics = getForNode(Description.Hostname, Status.Addr);
                return (
                  <span className="tabular-nums">
                    {metrics.cpu != null ? `${Math.round(metrics.cpu)}%` : "\u2014"}
                  </span>
                );
              },
            },
            {
              header: "Memory",
              cell: ({ Description, Status }) => {
                const metrics = getForNode(Description.Hostname, Status.Addr);
                return (
                  <span className="tabular-nums">
                    {metrics.memory != null ? `${Math.round(metrics.memory)}%` : "\u2014"}
                  </span>
                );
              },
            },
            {
              header: "CPU (1h)",
              cell: ({ Description, Status }) => {
                const metrics = getForNode(Description.Hostname, Status.Addr);
                if (metrics.cpuHistory.length > 1) {
                  return <Sparkline data={metrics.cpuHistory} />;
                }
                return <span className="text-muted-foreground">{"\u2014"}</span>;
              },
            },
          ]
        : [],
    [hasNodeExporter, getForNode],
  );

  const columns: Column<Node>[] = useMemo(
    () => [
      ...baseColumns,
      ...metricsColumns,
      {
        header: "Engine",
        cell: ({ Description }) => Description.Engine.EngineVersion,
      },
    ],
    [baseColumns, metricsColumns],
  );

  if (loading) {
    return (
      <div>
        <PageHeader title="Nodes" />
        <SkeletonTable columns={7} />
      </div>
    );
  }

  if (error) {
    return (
      <FetchError
        message={error.message}
        onRetry={retry}
      />
    );
  }

  return (
    <div>
      <PageHeader title="Nodes" />
      {hasNodeExporter && (
        <div className="mb-6 rounded-lg border bg-card p-4">
          <ErrorBoundary inline>
            <NodeResourceGauges />
          </ErrorBoundary>
        </div>
      )}
      {hasNodeExporter && (
        <div className="mb-6">
          <ErrorBoundary inline>
            <MetricsPanel
              header="Resource Utilization by Node"
              charts={[
                {
                  title: "CPU Utilization",
                  query: `100 - (avg by (instance)(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`,
                  unit: "%",
                  yMin: 0,
                  labelTransform: instanceToHostname,
                },
                {
                  title: "Memory Utilization",
                  query: `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`,
                  unit: "%",
                  yMin: 0,
                  labelTransform: instanceToHostname,
                },
              ]}
            />
          </ErrorBoundary>
        </div>
      )}
      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search nodes…"
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />
      {nodes.length === 0 ? (
        <EmptyState message={search ? "No nodes match your search" : "No nodes found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={nodes}
          keyFn={({ ID }) => ID}
          onRowClick={({ ID }) => navigate(`/nodes/${ID}`)}
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      ) : (
        <div className={cardGridClass}>
          {nodes.map((node) => {
            const metrics = getForNode(node.Description.Hostname, node.Status.Addr);

            return (
              <ResourceCard
                key={node.ID}
                title={node.Description.Hostname || node.ID}
                to={`/nodes/${node.ID}`}
                badge={<TaskStatusBadge state={node.Status.State} />}
                meta={[
                  node.ManagerStatus?.Leader ? (
                    <span
                      key="role"
                      className="flex items-center gap-1.5"
                    >
                      {node.Spec.Role}
                      <span className="rounded bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
                        Leader
                      </span>
                    </span>
                  ) : (
                    node.Spec.Role
                  ),
                  node.Spec.Availability,
                  `v${node.Description.Engine.EngineVersion}`,
                ]}
              >
                {hasNodeExporter && (
                  <div className="flex items-center justify-center gap-4">
                    <ResourceGauge
                      label="CPU"
                      value={metrics.cpu}
                      size="sm"
                    />
                    <ResourceGauge
                      label="Mem"
                      value={metrics.memory}
                      size="sm"
                    />
                    <ResourceGauge
                      label="Disk"
                      value={metrics.disk}
                      size="sm"
                    />
                  </div>
                )}
              </ResourceCard>
            );
          })}
        </div>
      )}
    </div>
  );
}
