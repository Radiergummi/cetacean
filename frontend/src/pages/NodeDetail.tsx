import { api } from "../api/client";
import type { HistoryEntry, Node, Task } from "../api/types";
import ActivitySection from "../components/ActivitySection";
import { MetadataGrid } from "../components/data";
import DiskUsageSection from "../components/DiskUsageSection";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { KeyValueEditor } from "../components/KeyValueEditor";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { MetricsPanel } from "../components/metrics";
import ResourceGauge from "../components/metrics/ResourceGauge";
import { AvailabilityEditor, EngineCard, OsCard, StatusCard } from "../components/node-detail";
import PageHeader from "../components/PageHeader";
import TasksTable from "../components/TasksTable";
import { useGaugeValue } from "../hooks/useGaugeValue";
import { useInstanceResolver } from "../hooks/useInstanceResolver";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { opsLevel, useOperationsLevel } from "../hooks/useOperationsLevel";
import { useResourceStream } from "../hooks/useResourceStream";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { formatBytes, formatNumber } from "../lib/format";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
import { escapePromQL } from "../lib/utils";
import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";

export default function NodeDetail() {
  const { id } = useParams<{ id: string }>();
  const [node, setNode] = useState<Node | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [nodeLabels, setNodeLabels] = useState<Record<string, string> | null>(null);

  const monitoring = useMonitoringStatus();
  const { level: operationsLevel, loading: levelLoading } = useOperationsLevel();
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
  const hasCadvisor = !!monitoring?.cadvisor?.targets;
  const { resolve } = useInstanceResolver();
  const [error, setError] = useState(false);

  const abortRef = useRef<AbortController | null>(null);

  const fetchData = useCallback(() => {
    if (!id) {
      return;
    }

    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    const { signal } = controller;

    api
      .node(id, signal)
      .then(setNode)
      .catch(() => {
        if (!signal.aborted) {
          setError(true);
        }
      });
    api
      .nodeTasks(id, signal)
      .then(setTasks)
      .catch(() => {});
    api
      .history({ resourceId: id, limit: 10 }, signal)
      .then(setHistory)
      .catch(() => {});
    api
      .nodeLabels(id, signal)
      .then(setNodeLabels)
      .catch(() => {});
  }, [id]);

  useEffect(() => {
    fetchData();
    return () => abortRef.current?.abort();
  }, [fetchData]);

  useResourceStream(`/nodes/${id}`, fetchData);

  const nodeId = node?.ID || "";
  const hostname = node?.Description?.Hostname || "";
  const instance = resolve(hostname) || "";
  const instanceFilter = instance
    ? `instance="${escapePromQL(instance)}"`
    : node?.Status?.Addr
      ? `instance=~"${escapePromQL(node.Status.Addr)}:.*"`
      : "";

  const taskMetrics = useTaskMetrics(
    nodeId ? `container_label_com_docker_swarm_node_id="${escapePromQL(nodeId)}"` : "",
    hasCadvisor && !!nodeId,
  );

  const gaugeEnabled = !!hasPrometheus && !!instanceFilter;
  const cpuGauge = useGaugeValue(
    `100 - (avg(rate(node_cpu_seconds_total{mode="idle",${instanceFilter}}[5m])) * 100)`,
    gaugeEnabled,
  );
  const memoryGauge = useGaugeValue(
    `(1 - node_memory_MemAvailable_bytes{${instanceFilter}} / node_memory_MemTotal_bytes{${instanceFilter}}) * 100`,
    gaugeEnabled,
  );
  const diskGauge = useGaugeValue(
    `max((1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs",${instanceFilter}} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|nsfs|squashfs",${instanceFilter}}) * 100)`,
    gaugeEnabled,
  );

  if (error) {
    return <FetchError message="Failed to load node" />;
  }

  if (!node) {
    return <LoadingDetail />;
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={node.Description.Hostname || node.ID}
        breadcrumbs={[
          { label: "Nodes", to: "/nodes" },
          { label: node.Description.Hostname || node.ID },
        ]}
      />

      <MetadataGrid>
        <InfoCard
          label="Role"
          value={
            <>
              <span className="capitalize">{node.Spec.Role}</span>
              {node.ManagerStatus?.Leader && (
                <span className="rounded bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
                  Leader
                </span>
              )}
            </>
          }
        />
        <StatusCard state={node.Status.State} />
        <AvailabilityEditor
          nodeId={node.ID}
          current={node.Spec.Availability}
        />
        <EngineCard version={node.Description.Engine.EngineVersion} />
        <OsCard
          os={node.Description.Platform.OS}
          architecture={node.Description.Platform.Architecture}
        />
        <InfoCard
          label="Address"
          value={
            <span className="inline-flex items-center">
              {node.Status.Addr || "\u2014"}
              {node.ManagerStatus?.Addr && (
                <span className="text-muted-foreground">
                  :{node.ManagerStatus.Addr.split(":").pop()}
                </span>
              )}
            </span>
          }
        />
        <InfoCard
          label="CPUs"
          value={formatNumber(node.Description.Resources.NanoCPUs / 1_000_000_000)}
          right={
            cpuGauge != null ? (
              <ResourceGauge
                label=""
                value={cpuGauge}
                size="sm"
              />
            ) : undefined
          }
        />
        <InfoCard
          label="Memory"
          value={formatBytes(node.Description.Resources.MemoryBytes)}
          right={
            memoryGauge != null ? (
              <ResourceGauge
                label=""
                value={memoryGauge}
                size="sm"
              />
            ) : undefined
          }
        />
        {diskGauge != null && (
          <InfoCard
            label="Disk"
            value={`${Math.round(diskGauge)}%`}
            right={
              <ResourceGauge
                label=""
                value={diskGauge}
                size="sm"
              />
            }
          />
        )}
      </MetadataGrid>

      {nodeLabels !== null && (
        <KeyValueEditor
          title="Labels"
          entries={nodeLabels}
          defaultOpen={Object.keys(nodeLabels).length > 0}
          keyPlaceholder="com.example.my-label"
          valuePlaceholder="value"
          editDisabled={levelLoading || operationsLevel < opsLevel.impactful}
          isKeyReadOnly={isReservedLabelKey}
          validateKey={validateLabelKey}
          onSave={async (ops) => {
            const updated = await api.patchNodeLabels(node.ID, ops);
            setNodeLabels(updated);
            return updated;
          }}
        />
      )}

      <TasksTable
        tasks={tasks}
        variant="node"
        metrics={hasCadvisor ? taskMetrics : undefined}
      />

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

      <DiskUsageSection nodeId={node.ID} />

      <ActivitySection
        entries={history}
        hideType
      />
    </div>
  );
}
