import { api } from "../api/client";
import type { Node } from "../api/types";
import type { Column } from "../components/DataTable";
import ErrorBoundary from "../components/ErrorBoundary";
import { ResourceGauge, Sparkline, NodeResourceGauges, MetricsPanel } from "../components/metrics";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import TaskStatusBadge from "../components/TaskStatusBadge";
import { isNodeExporterReady, useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useNodeMetrics } from "../hooks/useNodeMetrics";
import { instanceToHostname } from "../lib/format";
import { sortColumn } from "../lib/sortColumn";
import { useCallback } from "react";
import { Link } from "react-router-dom";

export default function NodeList() {
  const monitoring = useMonitoringStatus();
  const hasNodeExporter = isNodeExporterReady(monitoring);
  const { getForNode } = useNodeMetrics();

  const columns = useCallback(
    (sortKey: string | undefined, sortDir: "asc" | "desc", toggle: (key: string) => void) => {
      const base: Column<Node>[] = [
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
      ];

      const metrics: Column<Node>[] = hasNodeExporter
        ? [
            {
              header: "CPU",
              cell: ({ Description, Status }) => {
                const nodeMetrics = getForNode(Description.Hostname, Status.Addr);
                return (
                  <span className="tabular-nums">
                    {nodeMetrics.cpu != null ? `${Math.round(nodeMetrics.cpu)}%` : "\u2014"}
                  </span>
                );
              },
            },
            {
              header: "Memory",
              cell: ({ Description, Status }) => {
                const nodeMetrics = getForNode(Description.Hostname, Status.Addr);
                return (
                  <span className="tabular-nums">
                    {nodeMetrics.memory != null ? `${Math.round(nodeMetrics.memory)}%` : "\u2014"}
                  </span>
                );
              },
            },
            {
              header: "CPU (1h)",
              cell: ({ Description, Status }) => {
                const nodeMetrics = getForNode(Description.Hostname, Status.Addr);

                if (nodeMetrics.cpuHistory.length > 1) {
                  return <Sparkline data={nodeMetrics.cpuHistory} />;
                }

                return <span className="text-muted-foreground">{"\u2014"}</span>;
              },
            },
          ]
        : [];

      return [
        ...base,
        ...metrics,
        {
          header: "Engine",
          cell: ({ Description }: Node) => Description.Engine.EngineVersion,
        },
      ];
    },
    [hasNodeExporter, getForNode],
  );

  return (
    <ResourceListPage<Node>
      title="Nodes"
      path="/nodes"
      sseType="node"
      defaultSort="hostname"
      searchPlaceholder="Search nodes…"
      viewModeKey="nodes"
      fetchFn={(params, signal) => api.nodes(params, signal)}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `/nodes/${ID}`}
      columns={columns}
      skeletonColumns={7}
      headerContent={
        hasNodeExporter ? (
          <>
            <div className="mb-6 rounded-lg border bg-card p-4">
              <ErrorBoundary inline>
                <NodeResourceGauges />
              </ErrorBoundary>
            </div>
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
          </>
        ) : undefined
      }
      renderCard={(node) => {
        const metrics = getForNode(node.Description.Hostname, node.Status.Addr);

        return (
          <ResourceCard
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
      }}
      emptyMessage={(hasSearch) => (hasSearch ? "No nodes match your search" : "No nodes found")}
    />
  );
}
