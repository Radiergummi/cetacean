import { api } from "../api/client";
import type { Node } from "../api/types";
import ActivitySection from "../components/ActivitySection";
import { MetadataGrid } from "../components/data";
import DiskUsageSection from "../components/DiskUsageSection";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { KeyValueEditor } from "../components/KeyValueEditor";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { MetricsPanel, StackDrillDownChart } from "../components/metrics";
import ResourceGauge from "../components/metrics/ResourceGauge";
import {
  AvailabilityEditor,
  EngineCard,
  NodeActions,
  OsCard,
  RoleEditor,
  StatusCard,
} from "../components/node-detail";
import PageHeader from "../components/PageHeader";
import TasksTable from "../components/TasksTable";
import { useDetailResource } from "../hooks/useDetailResource";
import { useGaugeValue } from "../hooks/useGaugeValue";
import { useInstanceResolver } from "../hooks/useInstanceResolver";
import {
  isCadvisorReady,
  isPrometheusReady,
  useMonitoringStatus,
} from "../hooks/useMonitoringStatus";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { formatBytes, formatNumber } from "../lib/format";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
import { stackResourceCharts } from "../lib/stackQueries";
import { escapePromQL } from "../lib/utils";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";

function buildInstanceFilter(instance: string, address: string, hostname: string): string {
  if (instance) {
    return `instance="${escapePromQL(instance)}"`;
  }

  if (address) {
    return `instance=~"${escapePromQL(address)}:.*"`;
  }

  if (hostname) {
    return `instance=~"${escapePromQL(hostname)}(\\..+)?:.*"`;
  }

  return "";
}

export default function NodeDetail() {
  const { id } = useParams<{ id: string }>();

  const extraQueryKeys = useMemo(
    () =>
      [
        ["node-tasks", id],
        ["node-role", id],
      ] as const,
    [id],
  );

  const {
    data: node,
    history,
    error,
    allowedMethods,
  } = useDetailResource<Node>(id, api.node, `/nodes/${id}`, { extraQueryKeys });

  const { data: tasks } = useQuery({
    queryKey: ["node-tasks", id],
    queryFn: ({ signal }) => api.nodeTasks(id!, signal),
    enabled: !!id,
  });

  const { data: roleData } = useQuery({
    queryKey: ["node-role", id],
    queryFn: ({ signal }) => api.nodeRole(id!, signal),
    enabled: !!id,
  });

  // Local labels state, synced from server on every re-fetch and updated
  // optimistically after a successful patch.
  const [nodeLabels, setNodeLabels] = useState<Record<string, string>>({});

  useEffect(() => {
    if (node) {
      setNodeLabels(node.Spec.Labels ?? {});
    }
  }, [node]);

  const monitoring = useMonitoringStatus();
  const hasPrometheus = isPrometheusReady(monitoring);
  const hasCadvisor = isCadvisorReady(monitoring);
  const { resolve } = useInstanceResolver();

  const nodeId = node?.ID || "";
  const hostname = node?.Description?.Hostname || "";
  const instance = resolve(hostname) || "";
  const instanceFilter = buildInstanceFilter(instance, node?.Status?.Addr ?? "", hostname);

  const nodeStackCharts = stackResourceCharts(
    nodeId ? `container_label_com_docker_swarm_node_id="${escapePromQL(nodeId)}"` : "",
  );

  const taskMetrics = useTaskMetrics(
    nodeId ? `container_label_com_docker_swarm_node_id="${escapePromQL(nodeId)}"` : "",
    hasCadvisor && !!nodeId,
  );

  const gaugeEnabled = hasPrometheus && !!instanceFilter;
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

      <NodeActions
        node={node}
        allowedMethods={allowedMethods}
      />

      <MetadataGrid>
        <RoleEditor
          nodeId={node.ID}
          currentRole={node.Spec.Role}
          isLeader={node.ManagerStatus?.Leader ?? false}
          managerCount={roleData?.managerCount ?? null}
          canEdit={allowedMethods.has("PUT")}
        />
        <StatusCard state={node.Status.State} />
        <AvailabilityEditor
          nodeId={node.ID}
          current={node.Spec.Availability}
          canEdit={allowedMethods.has("PUT")}
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

      {node && (
        <KeyValueEditor
          title="Labels"
          entries={nodeLabels}
          defaultOpen={Object.keys(nodeLabels).length > 0}
          keyPlaceholder="com.example.my-label"
          valuePlaceholder="value"
          editDisabled={!allowedMethods.has("PATCH")}
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
        tasks={tasks ?? []}
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

      {hasCadvisor && nodeId && (
        <ErrorBoundary inline>
          <MetricsPanel
            header="Resource Usage by Stack"
            stackable
          >
            <StackDrillDownChart {...nodeStackCharts.cpu} />
            <StackDrillDownChart {...nodeStackCharts.memory} />
          </MetricsPanel>
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
