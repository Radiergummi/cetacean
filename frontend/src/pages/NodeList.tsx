import { useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSortParams } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useNodeMetrics } from "../hooks/useNodeMetrics";
import { useSearchParam } from "../hooks/useSearchParam";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { api } from "../api/client";
import type { Node } from "../api/types";
import ListToolbar from "../components/ListToolbar";
import PageHeader from "../components/PageHeader";
import DataTable, { type Column } from "../components/DataTable";
import SortIndicator from "../components/SortIndicator";
import TaskStatusBadge from "../components/TaskStatusBadge";
import ResourceCard from "../components/ResourceCard";
import { ResourceGauge, Sparkline, NodeResourceGauges } from "../components/metrics";
import EmptyState from "../components/EmptyState";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";

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
  const { getForNode } = useNodeMetrics();

  const baseColumns: Column<Node>[] = [
    {
      header: <SortIndicator label="Hostname" active={sortKey === "hostname"} dir={sortDir} />,
      cell: (node) => (
        <Link
          to={`/nodes/${node.ID}`}
          className="text-link hover:underline font-medium"
          onClick={(e) => e.stopPropagation()}
        >
          {node.Description.Hostname || node.ID}
        </Link>
      ),
      onHeaderClick: () => toggle("hostname"),
    },
    {
      header: <SortIndicator label="Role" active={sortKey === "role"} dir={sortDir} />,
      cell: (node) => node.Spec.Role,
      onHeaderClick: () => toggle("role"),
    },
    {
      header: <SortIndicator label="Status" active={sortKey === "status"} dir={sortDir} />,
      cell: (node) => <TaskStatusBadge state={node.Status.State} />,
      onHeaderClick: () => toggle("status"),
    },
  ];

  const metricsColumns: Column<Node>[] = hasPrometheus
    ? [
        {
          header: "CPU",
          cell: (node) => {
            const m = getForNode(node.Status.Addr);
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
            const m = getForNode(node.Status.Addr);
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
            const m = getForNode(node.Status.Addr);
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
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Nodes" />
      {hasPrometheus && (
        <div className="rounded-lg border bg-card p-4 mb-6">
          <ErrorBoundary inline>
            <NodeResourceGauges />
          </ErrorBoundary>
        </div>
      )}
      <ListToolbar search={search} onSearchChange={setSearch} placeholder="Search nodes..." viewMode={viewMode} onViewModeChange={setViewMode} />
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
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {nodes.map((node) => {
            const m = getForNode(node.Status.Addr);
            return (
              <ResourceCard
                key={node.ID}
                title={node.Description.Hostname || node.ID}
                to={`/nodes/${node.ID}`}
                badge={<TaskStatusBadge state={node.Status.State} />}
                meta={[
                  node.Spec.Role,
                  node.Spec.Availability,
                  `v${node.Description.Engine.EngineVersion}`,
                ]}
              >
                {hasPrometheus && (
                  <div className="flex items-center justify-center gap-4">
                    <ResourceGauge label="CPU" value={m.cpu} size="sm" />
                    <ResourceGauge label="Mem" value={m.memory} size="sm" />
                    <ResourceGauge label="Disk" value={m.disk} size="sm" />
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
