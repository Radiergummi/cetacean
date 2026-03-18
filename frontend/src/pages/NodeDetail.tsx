import { api } from "../api/client";
import type { HistoryEntry, Node, Task } from "../api/types";
import ActivitySection from "../components/ActivitySection";
import CollapsibleSection from "../components/CollapsibleSection";
import { MetadataGrid } from "../components/data";
import DiskUsageSection from "../components/DiskUsageSection";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { MetricsPanel, NodeResourceGauges } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import SimpleTable from "../components/SimpleTable";
import { Spinner } from "../components/Spinner";
import TasksTable from "../components/TasksTable";
import { useInstanceResolver } from "../hooks/useInstanceResolver";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useResourceStream } from "../hooks/useResourceStream";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { formatBytes } from "../lib/formatBytes";
import { escapePromQL } from "../lib/utils";
import { Pencil, Plus, Trash2, X } from "lucide-react";
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
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [newKey, setNewKey] = useState("");
  const [newVal, setNewVal] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  function openEdit() {
    setDraft({ ...labels });
    setNewKey("");
    setNewVal("");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addRow() {
    const k = newKey.trim();
    if (!k) return;
    setDraft((prev) => ({ ...prev, [k]: newVal }));
    setNewKey("");
    setNewVal("");
  }

  function removeRow(key: string) {
    if (!window.confirm(`Remove label "${key}"?`)) return;
    setDraft((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  }

  async function save() {
    const ops: Array<{ op: string; path: string; value?: string }> = [];
    const original = labels;

    for (const k of Object.keys(original)) {
      if (!(k in draft)) {
        ops.push({ op: "remove", path: `/${k}` });
      }
    }
    for (const [k, v] of Object.entries(draft)) {
      if (!(k in original)) {
        ops.push({ op: "add", path: `/${k}`, value: v });
      } else if (original[k] !== v) {
        ops.push({ op: "replace", path: `/${k}`, value: v });
      }
    }
    if (ops.length === 0) {
      setEditing(false);
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      const updated = await api.patchNodeLabels(nodeId, ops);
      onSaved(updated);
      setEditing(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const entries = Object.entries(labels).sort(([a], [b]) => a.localeCompare(b));
  const draftEntries = Object.entries(draft).sort(([a], [b]) => a.localeCompare(b));

  const controls = !editing ? (
    <button
      type="button"
      onClick={openEdit}
      className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs font-medium hover:bg-accent"
    >
      <Pencil className="h-3 w-3" />
      Edit
    </button>
  ) : null;

  return (
    <CollapsibleSection
      title="Labels"
      defaultOpen={false}
      controls={controls}
    >
      {!editing ? (
        entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">No labels.</p>
        ) : (
          <SimpleTable
            columns={["Key", "Value"]}
            items={entries}
            keyFn={([k]) => k}
            renderRow={([k, v]) => (
              <>
                <td className="p-3 font-mono text-xs">{k}</td>
                <td className="p-3 font-mono text-xs break-all">{v}</td>
              </>
            )}
          />
        )
      ) : (
        <div className="space-y-3">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-max whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="p-3 text-left text-sm font-medium">Key</th>
                  <th className="p-3 text-left text-sm font-medium">Value</th>
                  <th className="p-3" />
                </tr>
              </thead>
              <tbody>
                {draftEntries.map(([k, v]) => (
                  <tr
                    key={k}
                    className="border-b last:border-b-0"
                  >
                    <td className="p-3 font-mono text-xs">{k}</td>
                    <td className="p-2">
                      <input
                        type="text"
                        value={v}
                        onChange={(e) => setDraft((prev) => ({ ...prev, [k]: e.target.value }))}
                        className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:ring-1 focus:ring-ring focus:outline-none"
                      />
                    </td>
                    <td className="p-2">
                      <button
                        type="button"
                        onClick={() => removeRow(k)}
                        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-red-600"
                        title="Remove"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </td>
                  </tr>
                ))}
                <tr>
                  <td className="p-2">
                    <input
                      type="text"
                      value={newKey}
                      onChange={(e) => setNewKey(e.target.value)}
                      placeholder="key"
                      className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:ring-1 focus:ring-ring focus:outline-none"
                    />
                  </td>
                  <td className="p-2">
                    <input
                      type="text"
                      value={newVal}
                      onChange={(e) => setNewVal(e.target.value)}
                      placeholder="value"
                      onKeyDown={(e) => {
                        if (e.key === "Enter") addRow();
                      }}
                      className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:ring-1 focus:ring-ring focus:outline-none"
                    />
                  </td>
                  <td className="p-2">
                    <button
                      type="button"
                      onClick={addRow}
                      disabled={!newKey.trim()}
                      className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-40"
                      title="Add"
                    >
                      <Plus className="h-3.5 w-3.5" />
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void save()}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground disabled:opacity-50"
            >
              {saving && <Spinner className="size-3" />}
              Save
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded border px-3 py-1.5 text-xs font-medium disabled:opacity-50"
            >
              <X className="h-3 w-3" />
              Cancel
            </button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}

function NodeAvailabilityControl({ nodeId, current }: { nodeId: string; current: string }) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleChange(value: "active" | "drain" | "pause") {
    if (value === current) return;
    if (value === "drain") {
      if (!window.confirm("Draining this node will reschedule all running tasks. Continue?"))
        return;
    }
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

  return (
    <div className="flex flex-col items-start gap-1">
      <select
        value={current}
        disabled={loading}
        onChange={(e) => void handleChange(e.target.value as "active" | "drain" | "pause")}
        className="rounded border bg-background px-2 py-1 text-sm disabled:opacity-50"
      >
        <option value="active">Active</option>
        <option value="drain">Drain</option>
        <option value="pause">Pause</option>
      </select>
      {error && <p className="text-xs text-red-600 dark:text-red-400">{error}</p>}
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
          value={`${(node.Description.Resources.NanoCPUs / 1_000_000_000).toFixed(0)}`}
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
