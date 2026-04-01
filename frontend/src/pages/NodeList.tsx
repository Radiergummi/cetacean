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
import SortIndicator from "../components/SortIndicator";
import TaskStatusBadge from "../components/TaskStatusBadge";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useNodeMetrics } from "../hooks/useNodeMetrics";
import { useSearchParam } from "../hooks/useSearchParam";
import { useSortParams } from "../hooks/useSort";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { instanceToHostname } from "../lib/format";
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
  } = useSwarmResource(
    useCallback(
      (offset: number) =>
        api.nodes({ search: debouncedSearch, sort: sortKey, dir: sortDir, offset }),
      [debouncedSearch, sortKey, sortDir],
    ),
    "node",
    ({ ID }: Node) => ID,
  );
  const [viewMode, setViewMode] = useViewMode("nodes");
  const navigate = useNavigate();
  const monitoring = useMonitoringStatus();
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
  const hasNodeExporter = hasPrometheus && !!monitoring?.nodeExporter?.targets;
  const { getForNode } = useNodeMetrics();

  const baseColumns: Column<Node>[] = useMemo(
    () => [
      {
        header: (
          <SortIndicator
            label="Hostname"
            active={sortKey === "hostname"}
            dir={sortDir}
          />
        ),
        cell: ({ Description, ID }) => (
          <Link
            to={`/nodes/${ID}`}
            className="font-medium text-link hover:underline"
            onClick={(event) => event.stopPropagation()}
          >
            {Description.Hostname || ID}
          </Link>
        ),
        onHeaderClick: () => toggle("hostname"),
      },
      {
        header: (
          <SortIndicator
            label="Role"
            active={sortKey === "role"}
            dir={sortDir}
          />
        ),
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
        onHeaderClick: () => toggle("role"),
      },
      {
        header: (
          <SortIndicator
            label="Availability"
            active={sortKey === "availability"}
            dir={sortDir}
          />
        ),
        cell: ({ Spec }) => Spec.Availability,
        onHeaderClick: () => toggle("availability"),
      },
      {
        header: (
          <SortIndicator
            label="Status"
            active={sortKey === "status"}
            dir={sortDir}
          />
        ),
        cell: ({ Status }) => <TaskStatusBadge state={Status.State} />,
        onHeaderClick: () => toggle("status"),
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
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
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
