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
import { useCallback } from "react";
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
      () => api.nodes({ search: debouncedSearch, sort: sortKey, dir: sortDir }),
      [debouncedSearch, sortKey, sortDir],
    ),
    "node",
    (n: Node) => n.ID,
  );
  const [viewMode, setViewMode] = useViewMode("nodes");
  const navigate = useNavigate();
  const monitoring = useMonitoringStatus();
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
  const hasNodeExporter = hasPrometheus && !!monitoring?.nodeExporter?.targets;
  const { getForNode } = useNodeMetrics();

  const baseColumns: Column<Node>[] = [
    {
      header: (
        <SortIndicator
          label="Hostname"
          active={sortKey === "hostname"}
          dir={sortDir}
        />
      ),
      cell: (node) => (
        <Link
          to={`/nodes/${node.ID}`}
          className="font-medium text-link hover:underline"
          onClick={(e) => e.stopPropagation()}
        >
          {node.Description.Hostname || node.ID}
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
      cell: (node) => (
        <span className="flex items-center gap-1.5">
          {node.Spec.Role}
          {node.ManagerStatus?.Leader && (
            <span className="text-[10px] font-semibold tracking-wider text-amber-500 uppercase">
              leader
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
      cell: (node) => node.Spec.Availability,
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
      cell: (node) => <TaskStatusBadge state={node.Status.State} />,
      onHeaderClick: () => toggle("status"),
    },
    {
      header: "Address",
      cell: (node) => <span className="tabular-nums">{node.Status.Addr}</span>,
    },
  ];

  const metricsColumns: Column<Node>[] = hasPrometheus
    ? [
        {
          header: "CPU",
          cell: (node) => {
            const m = getForNode(node.Description.Hostname, node.Status.Addr);
            return (
              <span className="tabular-nums">
                {m.cpu != null ? `${Math.round(m.cpu)}%` : "\u2014"}
              </span>
            );
          },
        },
        {
          header: "Memory",
          cell: (node) => {
            const m = getForNode(node.Description.Hostname, node.Status.Addr);
            return (
              <span className="tabular-nums">
                {m.memory != null ? `${Math.round(m.memory)}%` : "\u2014"}
              </span>
            );
          },
        },
        {
          header: "CPU (1h)",
          cell: (node) => {
            const m = getForNode(node.Description.Hostname, node.Status.Addr);
            if (m.cpuHistory.length > 1) {
              return <Sparkline data={m.cpuHistory} />;
            }
            return <span className="text-muted-foreground">{"\u2014"}</span>;
          },
        },
      ]
    : [];

  const columns: Column<Node>[] = [
    ...baseColumns,
    ...metricsColumns,
    {
      header: "Engine",
      cell: (node) => node.Description.Engine.EngineVersion,
    },
  ];

  if (loading)
    return (
      <div>
        <PageHeader title="Nodes" />
        <SkeletonTable columns={7} />
      </div>
    );
  if (error)
    return (
      <FetchError
        message={error.message}
        onRetry={retry}
      />
    );

  return (
    <div>
      <PageHeader title="Nodes" />
      {hasPrometheus && (
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
                },
                {
                  title: "Memory Utilization",
                  query: `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`,
                  unit: "%",
                  yMin: 0,
                },
              ]}
            />
          </ErrorBoundary>
        </div>
      )}
      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search nodes..."
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />
      {nodes.length === 0 ? (
        <EmptyState message={search ? "No nodes match your search" : "No nodes found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={nodes}
          keyFn={(node) => node.ID}
          onRowClick={(node) => navigate(`/nodes/${node.ID}`)}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {nodes.map((node) => {
            const m = getForNode(node.Description.Hostname, node.Status.Addr);
            return (
              <ResourceCard
                key={node.ID}
                title={node.Description.Hostname || node.ID}
                to={`/nodes/${node.ID}`}
                badge={<TaskStatusBadge state={node.Status.State} />}
                meta={[
                  node.ManagerStatus?.Leader ? "leader" : node.Spec.Role,
                  node.Spec.Availability,
                  `v${node.Description.Engine.EngineVersion}`,
                ]}
              >
                {hasPrometheus && (
                  <div className="flex items-center justify-center gap-4">
                    <ResourceGauge
                      label="CPU"
                      value={m.cpu}
                      size="sm"
                    />
                    <ResourceGauge
                      label="Mem"
                      value={m.memory}
                      size="sm"
                    />
                    <ResourceGauge
                      label="Disk"
                      value={m.disk}
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
