import CollapsibleSection from "../CollapsibleSection";
import { Spinner } from "../Spinner";
import { api } from "@/api/client.ts";
import { formatBytes, formatCores } from "@/lib/format.ts";
import { Pencil, X } from "lucide-react";
import { useState } from "react";

interface ServiceResourceShape {
  limits?: { nanoCPUs?: number; memoryBytes?: number; pids?: number };
  reservations?: { nanoCPUs?: number; memoryBytes?: number };
}

export function ResourcesEditor({
  serviceId,
  resources,
  onSaved,
}: {
  serviceId: string;
  resources: Record<string, unknown>;
  onSaved: (updated: Record<string, unknown>) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const typed = resources as ServiceResourceShape;

  const [limitCpu, setLimitCpu] = useState("");
  const [limitMem, setLimitMem] = useState("");
  const [resCpu, setResCpu] = useState("");
  const [resMem, setResMem] = useState("");

  function openEdit() {
    setLimitCpu(typed.limits?.nanoCPUs != null ? String(typed.limits.nanoCPUs / 1e9) : "");
    setLimitMem(typed.limits?.memoryBytes != null ? String(typed.limits.memoryBytes) : "");
    setResCpu(
      typed.reservations?.nanoCPUs != null ? String(typed.reservations.nanoCPUs / 1e9) : "",
    );
    setResMem(
      typed.reservations?.memoryBytes != null ? String(typed.reservations.memoryBytes) : "",
    );
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  async function save() {
    const patch: ServiceResourceShape = {};
    if (limitCpu || limitMem) {
      patch.limits = {};
      if (limitCpu) {
        patch.limits.nanoCPUs = Math.round(parseFloat(limitCpu) * 1e9);
      }
      if (limitMem) {
        patch.limits.memoryBytes = parseInt(limitMem, 10);
      }
    }
    if (resCpu || resMem) {
      patch.reservations = {};
      if (resCpu) {
        patch.reservations.nanoCPUs = Math.round(parseFloat(resCpu) * 1e9);
      }
      if (resMem) {
        patch.reservations.memoryBytes = parseInt(resMem, 10);
      }
    }
    setSaving(true);
    setSaveError(null);
    try {
      const updated = await api.patchServiceResources(serviceId, patch);
      onSaved(updated);
      setEditing(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const hasResources =
    typed.limits?.nanoCPUs ||
    typed.limits?.memoryBytes ||
    typed.reservations?.nanoCPUs ||
    typed.reservations?.memoryBytes;

  const controls = !editing ? (
    <button
      type="button"
      onClick={openEdit}
      className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs font-medium hover:bg-accent"
    >
      <Pencil className="size-3" />
      Edit
    </button>
  ) : null;

  return (
    <CollapsibleSection
      title="Resource Limits"
      defaultOpen={false}
      controls={controls}
    >
      {!editing ? (
        !hasResources ? (
          <p className="text-sm text-muted-foreground">No resource limits configured.</p>
        ) : (
          <div className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
            {typed.limits?.nanoCPUs != null && (
              <div>
                <div className="text-xs text-muted-foreground">CPU Limit</div>
                <div className="font-mono">{formatCores(typed.limits.nanoCPUs / 1e9)}</div>
              </div>
            )}
            {typed.limits?.memoryBytes != null && (
              <div>
                <div className="text-xs text-muted-foreground">Memory Limit</div>
                <div className="font-mono">{formatBytes(typed.limits.memoryBytes)}</div>
              </div>
            )}
            {typed.reservations?.nanoCPUs != null && (
              <div>
                <div className="text-xs text-muted-foreground">CPU Reserved</div>
                <div className="font-mono">{formatCores(typed.reservations.nanoCPUs / 1e9)}</div>
              </div>
            )}
            {typed.reservations?.memoryBytes != null && (
              <div>
                <div className="text-xs text-muted-foreground">Memory Reserved</div>
                <div className="font-mono">{formatBytes(typed.reservations.memoryBytes)}</div>
              </div>
            )}
          </div>
        )
      ) : (
        <div className="space-y-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border p-3">
              <h4 className="text-xs font-medium text-muted-foreground uppercase">Limits</h4>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">CPU (cores)</span>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={limitCpu}
                  onChange={(e) => setLimitCpu(e.target.value)}
                  placeholder="e.g. 0.5"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:ring-1 focus:ring-ring focus:outline-none"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">Memory (bytes)</span>
                <input
                  type="number"
                  min="0"
                  value={limitMem}
                  onChange={(e) => setLimitMem(e.target.value)}
                  placeholder="e.g. 536870912"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:ring-1 focus:ring-ring focus:outline-none"
                />
              </label>
            </div>
            <div className="space-y-2 rounded-lg border p-3">
              <h4 className="text-xs font-medium text-muted-foreground uppercase">Reservations</h4>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">CPU (cores)</span>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={resCpu}
                  onChange={(e) => setResCpu(e.target.value)}
                  placeholder="e.g. 0.25"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:ring-1 focus:ring-ring focus:outline-none"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">Memory (bytes)</span>
                <input
                  type="number"
                  min="0"
                  value={resMem}
                  onChange={(e) => setResMem(e.target.value)}
                  placeholder="e.g. 268435456"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:ring-1 focus:ring-ring focus:outline-none"
                />
              </label>
            </div>
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
              <X className="size-3" />
              Cancel
            </button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}
