import { ResourceRangeSlider } from "./resource-range-slider";
import { api } from "@/api/client";
import type { ClusterCapacity } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { formatBytes, formatCores } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { useState } from "react";

export interface ServiceResourceShape {
  limits?: { nanoCPUs?: number; memoryBytes?: number; pids?: number };
  reservations?: { nanoCPUs?: number; memoryBytes?: number };
}

export function ResourcesEditor({
  serviceId,
  resources,
  pids,
  onSaved,
}: {
  serviceId: string;
  resources: ServiceResourceShape;
  pids?: number;
  onSaved: (updated: Record<string, unknown>) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const [cpu, setCpu] = useState<{ reservation: number | undefined; limit: number | undefined }>({
    reservation: undefined,
    limit: undefined,
  });
  const [memory, setMemory] = useState<{
    reservation: number | undefined;
    limit: number | undefined;
  }>({
    reservation: undefined,
    limit: undefined,
  });
  const [capacity, setCapacity] = useState<ClusterCapacity | null>(null);

  function openEdit() {
    setCpu({
      reservation:
        resources.reservations?.nanoCPUs != null
          ? resources.reservations.nanoCPUs / 1e9
          : undefined,
      limit: resources.limits?.nanoCPUs != null ? resources.limits.nanoCPUs / 1e9 : undefined,
    });
    setMemory({
      reservation:
        resources.reservations?.memoryBytes != null
          ? resources.reservations.memoryBytes / (1024 * 1024)
          : undefined,
      limit:
        resources.limits?.memoryBytes != null
          ? resources.limits.memoryBytes / (1024 * 1024)
          : undefined,
    });
    api
      .clusterCapacity()
      .then(setCapacity)
      .catch(() => {});
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  async function save() {
    const patch: ServiceResourceShape = {};
    if (cpu.limit !== undefined || memory.limit !== undefined) {
      patch.limits = {};
      if (cpu.limit !== undefined) patch.limits.nanoCPUs = Math.round(cpu.limit * 1e9);
      if (memory.limit !== undefined)
        patch.limits.memoryBytes = Math.round(memory.limit * 1024 * 1024);
    }
    if (cpu.reservation !== undefined || memory.reservation !== undefined) {
      patch.reservations = {};
      if (cpu.reservation !== undefined)
        patch.reservations.nanoCPUs = Math.round(cpu.reservation * 1e9);
      if (memory.reservation !== undefined)
        patch.reservations.memoryBytes = Math.round(memory.reservation * 1024 * 1024);
    }
    setSaving(true);
    setSaveError(null);
    try {
      const updated = await api.patchServiceResources(serviceId, patch);
      onSaved(updated);
      setEditing(false);
    } catch (err) {
      setSaveError(getErrorMessage(err, "Save failed"));
    } finally {
      setSaving(false);
    }
  }

  const hasResources =
    resources.limits?.nanoCPUs != null ||
    resources.limits?.memoryBytes != null ||
    resources.reservations?.nanoCPUs != null ||
    resources.reservations?.memoryBytes != null;

  if (!editing) {
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          {!hasResources && pids == null ? (
            <p className="text-sm text-muted-foreground">No resource limits configured.</p>
          ) : (
            <div className="grid flex-1 grid-cols-2 gap-4 text-sm sm:grid-cols-4">
              {resources.limits?.nanoCPUs != null && (
                <div>
                  <div className="text-xs text-muted-foreground">CPU Limit</div>
                  <div className="font-mono">{formatCores(resources.limits.nanoCPUs / 1e9)}</div>
                </div>
              )}
              {resources.limits?.memoryBytes != null && (
                <div>
                  <div className="text-xs text-muted-foreground">Memory Limit</div>
                  <div className="font-mono">{formatBytes(resources.limits.memoryBytes)}</div>
                </div>
              )}
              {resources.reservations?.nanoCPUs != null && (
                <div>
                  <div className="text-xs text-muted-foreground">CPU Reserved</div>
                  <div className="font-mono">
                    {formatCores(resources.reservations.nanoCPUs / 1e9)}
                  </div>
                </div>
              )}
              {resources.reservations?.memoryBytes != null && (
                <div>
                  <div className="text-xs text-muted-foreground">Memory Reserved</div>
                  <div className="font-mono">{formatBytes(resources.reservations.memoryBytes)}</div>
                </div>
              )}
            </div>
          )}
          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        </div>
        {pids != null && (
          <div className="flex items-center justify-between text-sm">
            <span className="font-medium">PID Limit</span>
            <span className="font-mono">{pids}</span>
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {capacity ? (
        <div className="space-y-3">
          <ResourceRangeSlider
            label="CPU (cores)"
            reservation={cpu.reservation}
            limit={cpu.limit}
            onChange={setCpu}
            max={capacity.maxNodeCPU}
            step={0.25}
          />
          <ResourceRangeSlider
            label="Memory (MB)"
            reservation={memory.reservation}
            limit={memory.limit}
            onChange={setMemory}
            max={capacity.maxNodeMemory / (1024 * 1024)}
            step={16}
          />
        </div>
      ) : (
        <div className="h-24 animate-pulse rounded bg-muted" />
      )}

      {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

      <footer className="flex items-center justify-end gap-2">
        <Button
          size="sm"
          onClick={() => void save()}
          disabled={saving}
        >
          {saving && <Spinner className="size-3" />}
          Save
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={cancelEdit}
          disabled={saving}
        >
          Cancel
        </Button>
      </footer>
    </div>
  );
}
