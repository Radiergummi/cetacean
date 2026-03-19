import { ResourceRangeSlider } from "./resource-range-slider";
import { api } from "@/api/client";
import type { ClusterCapacity } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { formatBytes, formatCores, formatNumber } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { useState } from "react";

const formatCpuTick = (cores: number) => formatNumber(cores, 2);
const formatMemoryTick = (megabytes: number) => formatBytes(megabytes * 1024 * 1024);

export interface ServiceResourceShape {
  Limits?: { NanoCPUs?: number; MemoryBytes?: number; Pids?: number };
  Reservations?: { NanoCPUs?: number; MemoryBytes?: number };
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
        resources.Reservations?.NanoCPUs != null
          ? resources.Reservations.NanoCPUs / 1e9
          : undefined,
      limit: resources.Limits?.NanoCPUs != null ? resources.Limits.NanoCPUs / 1e9 : undefined,
    });
    setMemory({
      reservation:
        resources.Reservations?.MemoryBytes != null
          ? resources.Reservations.MemoryBytes / (1024 * 1024)
          : undefined,
      limit:
        resources.Limits?.MemoryBytes != null
          ? resources.Limits.MemoryBytes / (1024 * 1024)
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
      patch.Limits = {};
      if (cpu.limit !== undefined) patch.Limits.NanoCPUs = Math.round(cpu.limit * 1e9);
      if (memory.limit !== undefined)
        patch.Limits.MemoryBytes = Math.round(memory.limit * 1024 * 1024);
    }
    if (cpu.reservation !== undefined || memory.reservation !== undefined) {
      patch.Reservations = {};
      if (cpu.reservation !== undefined)
        patch.Reservations.NanoCPUs = Math.round(cpu.reservation * 1e9);
      if (memory.reservation !== undefined)
        patch.Reservations.MemoryBytes = Math.round(memory.reservation * 1024 * 1024);
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
    resources.Limits?.NanoCPUs != null ||
    resources.Limits?.MemoryBytes != null ||
    resources.Reservations?.NanoCPUs != null ||
    resources.Reservations?.MemoryBytes != null;

  if (!editing) {
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          {!hasResources && pids == null ? (
            <p className="text-sm text-muted-foreground">No resource limits configured.</p>
          ) : (
            <div className="grid flex-1 grid-cols-2 gap-4 text-sm sm:grid-cols-4">
              {resources.Limits?.NanoCPUs != null && (
                <div>
                  <div className="text-xs text-muted-foreground">CPU Limit</div>
                  <div className="font-mono">{formatCores(resources.Limits.NanoCPUs / 1e9)}</div>
                </div>
              )}
              {resources.Limits?.MemoryBytes != null && (
                <div>
                  <div className="text-xs text-muted-foreground">Memory Limit</div>
                  <div className="font-mono">{formatBytes(resources.Limits.MemoryBytes)}</div>
                </div>
              )}
              {resources.Reservations?.NanoCPUs != null && (
                <div>
                  <div className="text-xs text-muted-foreground">CPU Reserved</div>
                  <div className="font-mono">
                    {formatCores(resources.Reservations.NanoCPUs / 1e9)}
                  </div>
                </div>
              )}
              {resources.Reservations?.MemoryBytes != null && (
                <div>
                  <div className="text-xs text-muted-foreground">Memory Reserved</div>
                  <div className="font-mono">{formatBytes(resources.Reservations.MemoryBytes)}</div>
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
            formatLabel={formatCpuTick}
          />
          <ResourceRangeSlider
            label="Memory"
            reservation={memory.reservation}
            limit={memory.limit}
            onChange={setMemory}
            max={capacity.maxNodeMemory / (1024 * 1024)}
            step={16}
            formatLabel={formatMemoryTick}
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
