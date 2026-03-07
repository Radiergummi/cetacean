import { useState, useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSort } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useNodeMetrics } from "../hooks/useNodeMetrics";
import { api } from "../api/client";
import type { Node } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import SortableHeader from "../components/SortableHeader";
import ViewToggle from "../components/ViewToggle";
import TaskStatusBadge from "../components/TaskStatusBadge";
import ResourceGauge from "../components/ResourceGauge";
import Sparkline from "../components/Sparkline";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { LoadingPage } from "../components/LoadingSkeleton";
import NodeResourceGauges from "../components/NodeResourceGauges";

const sortAccessors = {
  hostname: (n: Node) => n.Description.Hostname,
  role: (n: Node) => n.Spec.Role,
  status: (n: Node) => n.Status.State,
  availability: (n: Node) => n.Spec.Availability,
  engine: (n: Node) => n.Description.Engine.EngineVersion,
};

export default function NodeList() {
  const {
    data: nodes,
    loading,
    error,
    retry,
  } = useSwarmResource(api.nodes, "node", (n: Node) => n.ID);
  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode("nodes");
  const navigate = useNavigate();
  const { getForNode } = useNodeMetrics();
  const filtered = useMemo(
    () => nodes.filter((n) => n.Description.Hostname.toLowerCase().includes(search.toLowerCase())),
    [nodes, search],
  );
  const { sorted, sortKey, sortDir, toggle } = useSort(filtered, sortAccessors, "hostname");

  if (loading) return <LoadingPage />;
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Nodes" />
      <div className="rounded-lg border bg-card p-4 mb-6">
        <NodeResourceGauges />
      </div>
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search nodes..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {sorted.length === 0 ? (
        <EmptyState message={search ? "No nodes match your search" : "No nodes found"} />
      ) : viewMode === "table" ? (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full">
            <thead className="sticky top-0 z-10 bg-background">
              <tr className="border-b bg-muted/50">
                <SortableHeader
                  label="Hostname"
                  sortKey="hostname"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Role"
                  sortKey="role"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Status"
                  sortKey="status"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <th className="text-left p-3 text-sm font-medium">CPU</th>
                <th className="text-left p-3 text-sm font-medium">Memory</th>
                <th className="text-left p-3 text-sm font-medium">CPU (1h)</th>
                <SortableHeader
                  label="Engine"
                  sortKey="engine"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
              </tr>
            </thead>
            <tbody>
              {sorted.map((node) => {
                const m = getForNode(node.Status.Addr);
                return (
                  <tr
                    key={node.ID}
                    className="border-b cursor-pointer hover:bg-muted/50"
                    onClick={() => navigate(`/nodes/${node.ID}`)}
                  >
                    <td className="p-3">
                      <Link
                        to={`/nodes/${node.ID}`}
                        className="text-link hover:underline font-medium"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {node.Description.Hostname || node.ID}
                      </Link>
                    </td>
                    <td className="p-3 text-sm">{node.Spec.Role}</td>
                    <td className="p-3 text-sm">
                      <TaskStatusBadge state={node.Status.State} />
                    </td>
                    <td className="p-3 text-sm tabular-nums">
                      {m.cpu != null ? `${Math.round(m.cpu)}%` : "\u2014"}
                    </td>
                    <td className="p-3 text-sm tabular-nums">
                      {m.memory != null ? `${Math.round(m.memory)}%` : "\u2014"}
                    </td>
                    <td className="p-3">
                      {m.cpuHistory.length > 1 ? (
                        <Sparkline data={m.cpuHistory} />
                      ) : (
                        <span className="text-sm text-muted-foreground">{"\u2014"}</span>
                      )}
                    </td>
                    <td className="p-3 text-sm">{node.Description.Engine.EngineVersion}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {sorted.map((node) => {
            const m = getForNode(node.Status.Addr);
            return (
              <Link
                key={node.ID}
                to={`/nodes/${node.ID}`}
                className="rounded-lg border bg-card p-4 hover:border-foreground/20 hover:shadow-sm transition-all"
              >
                <div className="flex items-center justify-between mb-3">
                  <span className="font-medium truncate">
                    {node.Description.Hostname || node.ID}
                  </span>
                  <TaskStatusBadge state={node.Status.State} />
                </div>
                <div className="flex items-center justify-center gap-4 mb-3">
                  <ResourceGauge label="CPU" value={m.cpu} size="sm" />
                  <ResourceGauge label="Mem" value={m.memory} size="sm" />
                  <ResourceGauge label="Disk" value={m.disk} size="sm" />
                </div>
                <div className="flex items-center gap-3 text-xs text-muted-foreground">
                  <span>{node.Spec.Role}</span>
                  <span>{node.Spec.Availability}</span>
                  <span>v{node.Description.Engine.EngineVersion}</span>
                </div>
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}
