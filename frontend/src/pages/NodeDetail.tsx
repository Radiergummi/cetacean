import { useCallback, useEffect, useState } from "react";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { escapePromQL } from "../lib/utils";
import { useParams } from "react-router-dom";
import { api } from "../api/client";
import type { HistoryEntry, Node, Task } from "../api/types";
import ActivitySection from "../components/ActivitySection";
import DiskUsageSection from "../components/DiskUsageSection";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { LabelSection, MetadataGrid } from "../components/data";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { MetricsPanel, NodeResourceGauges } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import TasksTable from "../components/TasksTable";
import { useInstanceResolver } from "../hooks/useInstanceResolver";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useResourceStream } from "../hooks/useResourceStream";
import { formatBytes } from "../lib/formatBytes";

export default function NodeDetail() {
  const { id } = useParams<{ id: string }>();
  const [node, setNode] = useState<Node | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);

  const monitoring = useMonitoringStatus();
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
  const hasCadvisor = !!monitoring?.cadvisor?.targets;
  const { resolve } = useInstanceResolver();
  const [error, setError] = useState(false);

  const fetchData = useCallback(() => {
    if (!id) {
      return;
    }

    api
      .node(id)
      .then(setNode)
      .catch(() => setError(true));
    api
      .nodeTasks(id)
      .then(setTasks)
      .catch(() => {});
    api
      .history({ resourceId: id, limit: 10 })
      .then(setHistory)
      .catch(() => {});
  }, [id]);

  useEffect(fetchData, [fetchData]);

  useResourceStream(`/nodes/${id}`, fetchData);

  const nodeId = node?.ID || "";
  const taskMetrics = useTaskMetrics(
    nodeId ? `container_label_com_docker_swarm_node_id="${escapePromQL(nodeId)}"` : "",
    hasCadvisor && !!nodeId,
  );

  if (error) {
    return <FetchError message="Failed to load node" />;
  }

  if (!node) {
    return <LoadingDetail />;
  }

  const hostname = node.Description.Hostname || "";
  const instance = resolve(hostname) || "";
  const instanceFilter = instance
    ? `instance="${escapePromQL(instance)}"`
    : `instance=~"${escapePromQL(node.Status.Addr)}:.*"`;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={node.Description.Hostname || node.ID}
        breadcrumbs={[
          { label: "Nodes", to: "/nodes" },
          { label: node.Description.Hostname || node.ID },
        ]}
      />

      {hasPrometheus && (
        <div className="rounded-lg border bg-card p-4">
          <ErrorBoundary inline>
            <NodeResourceGauges instance={instance || undefined} />
          </ErrorBoundary>
        </div>
      )}

      <MetadataGrid>
        <InfoCard label="Role" value={node.Spec.Role} />
        <InfoCard label="Status" value={node.Status.State} />
        <InfoCard label="Availability" value={node.Spec.Availability} />
        <InfoCard label="Engine" value={node.Description.Engine.EngineVersion} />
        <InfoCard
          label="OS"
          value={`${node.Description.Platform.OS} ${node.Description.Platform.Architecture}`}
        />
        <InfoCard label="Address" value={node.Status.Addr || ""} />
        <InfoCard
          label="CPUs"
          value={`${(node.Description.Resources.NanoCPUs / 1_000_000_000).toFixed(0)}`}
        />
        <InfoCard label="Memory" value={formatBytes(node.Description.Resources.MemoryBytes)} />

        {node.ManagerStatus && (
          <>
            <InfoCard label="Manager" value={node.ManagerStatus.Leader ? "Leader" : "Reachable"} />
            <InfoCard label="Manager Address" value={node.ManagerStatus.Addr} />
          </>
        )}
      </MetadataGrid>

      {node.Spec.Labels && Object.keys(node.Spec.Labels).length > 0 && (
        <LabelSection
          entries={Object.entries(node.Spec.Labels).sort(([a], [b]) => a.localeCompare(b))}
        />
      )}

      <TasksTable tasks={tasks} variant="node" metrics={hasCadvisor ? taskMetrics : undefined} />

      <DiskUsageSection nodeId={node.ID} />

      <ActivitySection entries={history} />

      {hasPrometheus && (
        <ErrorBoundary inline>
          <MetricsPanel
            header="Metrics"
            charts={[
              {
                title: "CPU Usage",
                query: `100 - (avg(rate(node_cpu_seconds_total{mode="idle",${instanceFilter}}[5m])) * 100)`,
                unit: "%",
              },
              {
                title: "Memory Usage",
                query: `(1 - node_memory_MemAvailable_bytes{${instanceFilter}} / node_memory_MemTotal_bytes{${instanceFilter}}) * 100`,
                unit: "%",
              },
              {
                title: "Disk I/O",
                query: `sum(rate(node_disk_read_bytes_total{${instanceFilter}}[5m]))`,
                unit: "bytes/s",
              },
              {
                title: "Network I/O",
                query: `sum(rate(node_network_receive_bytes_total{device!="lo",${instanceFilter}}[5m]))`,
                unit: "bytes/s",
              },
            ]}
          />
        </ErrorBoundary>
      )}
    </div>
  );
}
