import { api } from "../api/client";
import type { HistoryEntry, Node, PatchOp, Task } from "../api/types";
import ActivitySection from "../components/ActivitySection";
import { MetadataGrid } from "../components/data";
import DiskUsageSection from "../components/DiskUsageSection";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { KeyValueEditor } from "../components/KeyValueEditor";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { MetricsPanel, NodeResourceGauges } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import TasksTable from "../components/TasksTable";
import { useInstanceResolver } from "../hooks/useInstanceResolver";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useResourceStream } from "../hooks/useResourceStream";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { formatBytes, formatNumber } from "../lib/format";
import { escapePromQL } from "../lib/utils";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useCallback, useEffect, useState } from "react";
import { useParams } from "react-router-dom";

function LabelsEditor({
  nodeId,
  labels,
  onSaved,
}: {
  nodeId: string;
  labels: Record<string, string>;
  onSaved: (updated: Record<string, string>) => void;
}) {
  async function handleSave(ops: PatchOp[]) {
    const updated = await api.patchNodeLabels(nodeId, ops);
    onSaved(updated);
    return updated;
  }

  return (
    <KeyValueEditor
      title="Labels"
      entries={labels}
      keyPlaceholder="key"
      valuePlaceholder="value"
      onSave={handleSave}
    />
  );
}

function NodeAvailabilityControl({ nodeId, current }: { nodeId: string; current: string }) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [drainPending, setDrainPending] = useState(false);

  async function handleChange(value: "active" | "drain" | "pause") {
    if (value === current) return;
    setLoading(true);
    setError(null);
    try {
      await api.updateNodeAvailability(nodeId, value);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to update availability");
    } finally {
      setLoading(false);
    }
  }

  function handleValueChange(value: string | null) {
    if (value === null) return;
    if (value === "drain" && current !== "drain") {
      setDrainPending(true);
    } else {
      void handleChange(value as "active" | "drain" | "pause");
    }
  }

  function confirmDrain() {
    setDrainPending(false);
    void handleChange("drain");
  }

  return (
    <div className="flex flex-col items-start gap-1">
      <Select
        value={current}
        onValueChange={handleValueChange}
        disabled={loading}
      >
        <SelectTrigger className="w-32">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="active">Active</SelectItem>
          <SelectItem value="drain">Drain</SelectItem>
          <SelectItem value="pause">Pause</SelectItem>
        </SelectContent>
      </Select>
      {error && <p className="text-xs text-red-600 dark:text-red-400">{error}</p>}
      <AlertDialog
        open={drainPending}
        onOpenChange={setDrainPending}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Drain this node?</AlertDialogTitle>
            <AlertDialogDescription>
              Draining this node will reschedule all running tasks to other nodes.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDrain}>Drain</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

export default function NodeDetail() {
  const { id } = useParams<{ id: string }>();
  const [node, setNode] = useState<Node | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [nodeLabels, setNodeLabels] = useState<Record<string, string> | null>(null);

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
    api
      .nodeLabels(id)
      .then(setNodeLabels)
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
        <InfoCard
          label="Role"
          value={node.Spec.Role}
        />
        <InfoCard
          label="Status"
          value={node.Status.State}
        />
        <div className="rounded-lg border bg-card p-4">
          <div className="mb-2 text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Availability
          </div>
          <NodeAvailabilityControl
            nodeId={node.ID}
            current={node.Spec.Availability}
          />
        </div>
        <InfoCard
          label="Engine"
          value={node.Description.Engine.EngineVersion}
        />
        <InfoCard
          label="OS"
          value={`${node.Description.Platform.OS} ${node.Description.Platform.Architecture}`}
        />
        <InfoCard
          label="Address"
          value={node.Status.Addr || ""}
        />
        <InfoCard
          label="CPUs"
          value={formatNumber(node.Description.Resources.NanoCPUs / 1_000_000_000)}
        />
        <InfoCard
          label="Memory"
          value={formatBytes(node.Description.Resources.MemoryBytes)}
        />

        {node.ManagerStatus && (
          <>
            <InfoCard
              label="Manager"
              value={node.ManagerStatus.Leader ? "Leader" : "Reachable"}
            />
            <InfoCard
              label="Manager Address"
              value={node.ManagerStatus.Addr}
            />
          </>
        )}
      </MetadataGrid>

      {nodeLabels !== null && (
        <LabelsEditor
          nodeId={node.ID}
          labels={nodeLabels}
          onSaved={setNodeLabels}
        />
      )}

      <TasksTable
        tasks={tasks}
        variant="node"
        metrics={hasCadvisor ? taskMetrics : undefined}
      />

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
