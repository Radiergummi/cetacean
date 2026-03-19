import { ResourceRangeSlider } from "./resource-range-slider";
import { api } from "@/api/client";
import type { ClusterCapacity } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { formatBytes, formatCores, formatNumber, formatPercentage } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { useState } from "react";

const formatCpuTick = (cores: number) => formatNumber(cores, 2);
const formatMemoryTick = (megabytes: number) => formatBytes(megabytes * 1024 * 1024);

/** Compute a slider max that keeps current values visible while capping at node capacity. */
function sliderMax(
  reservation: number | undefined,
  limit: number | undefined,
  nodeCapacity: number,
  step: number,
): number {
  const highestValue = Math.max(reservation ?? 0, limit ?? 0);
  if (highestValue === 0) return nodeCapacity;
  // Show at least 2x the highest value, rounded up to the next step, capped at node capacity
  const desired = Math.ceil((highestValue * 2) / step) * step;
  return Math.min(Math.max(desired, step * 8), nodeCapacity);
}

export interface ServiceResourceShape {
  Limits?: { NanoCPUs?: number; MemoryBytes?: number; Pids?: number };
  Reservations?: { NanoCPUs?: number; MemoryBytes?: number };
}

interface AllocationData {
  cpuReserved?: number;
  cpuLimit?: number;
  cpuActual?: number;
  memReserved?: number;
  memLimit?: number;
  memActual?: number;
}

export function ResourcesEditor({
  serviceId,
  resources,
  pids,
  allocation,
  onSaved,
}: {
  serviceId: string;
  resources: ServiceResourceShape;
  pids?: number;
  allocation?: AllocationData;
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

  // Only show allocation bars when actual usage data is available (requires Prometheus).
  // Without actual data, the text grid is a better fit.
  const hasActualUsage = allocation?.cpuActual != null || allocation?.memActual != null;

  if (!editing) {
    return (
      <div className="space-y-3">
        {!hasResources && pids == null ? (
          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">No resource limits configured.</p>
            <Button
              variant="outline"
              size="xs"
              onClick={openEdit}
            >
              <Pencil className="size-3" />
              Edit
            </Button>
          </div>
        ) : (
          <>
            <div className="flex items-center justify-end">
              <Button
                variant="outline"
                size="xs"
                onClick={openEdit}
              >
                <Pencil className="size-3" />
                Edit
              </Button>
            </div>
            {hasActualUsage && allocation ? (
              <div className="space-y-3">
                {(allocation.cpuReserved != null ||
                  allocation.cpuActual != null ||
                  allocation.cpuLimit != null) && (
                  <AllocationBar
                    label="CPU"
                    reserved={allocation.cpuReserved}
                    actual={allocation.cpuActual}
                    limit={allocation.cpuLimit}
                    formatValue={formatPercentage}
                  />
                )}
                {(allocation.memReserved != null ||
                  allocation.memActual != null ||
                  allocation.memLimit != null) && (
                  <AllocationBar
                    label="Memory"
                    reserved={allocation.memReserved}
                    actual={allocation.memActual}
                    limit={allocation.memLimit}
                    formatValue={formatBytes}
                  />
                )}
              </div>
            ) : (
              <div className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
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
                    <div className="font-mono">
                      {formatBytes(resources.Reservations.MemoryBytes)}
                    </div>
                  </div>
                )}
              </div>
            )}
          </>
        )}
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
            max={sliderMax(cpu.reservation, cpu.limit, capacity.maxNodeCPU, 0.25)}
            step={0.25}
            formatLabel={formatCpuTick}
          />
          <ResourceRangeSlider
            label="Memory"
            reservation={memory.reservation}
            limit={memory.limit}
            onChange={setMemory}
            max={sliderMax(
              memory.reservation,
              memory.limit,
              capacity.maxNodeMemory / (1024 * 1024),
              16,
            )}
            step={16}
            unit="MB"
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

function AllocationBar({
  label,
  reserved,
  actual,
  limit,
  formatValue,
}: {
  label: string;
  reserved?: number;
  actual?: number;
  limit?: number;
  formatValue: (value: number) => string;
}) {
  const maxValue = Math.max(reserved ?? 0, actual ?? 0, limit ?? 0) * 1.15;
  if (maxValue === 0) return null;

  const actualPercent = actual != null ? (actual / maxValue) * 100 : 0;
  const reservedPercent = reserved != null ? (reserved / maxValue) * 100 : undefined;
  const limitPercent = limit != null ? (limit / maxValue) * 100 : undefined;

  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-xs">
        <span className="font-medium">{label}</span>
        <div className="flex gap-3 text-muted-foreground">
          {actual != null && (
            <span>
              <span className="font-mono text-foreground">{formatValue(actual)}</span>
            </span>
          )}
          {reserved != null && (
            <span>
              Rsv: <span className="font-mono text-foreground">{formatValue(reserved)}</span>
            </span>
          )}
          {limit != null && (
            <span>
              Lim: <span className="font-mono text-foreground">{formatValue(limit)}</span>
            </span>
          )}
        </div>
      </div>
      <div className="relative h-1.5 rounded-full bg-muted">
        {/* Actual usage bar */}
        {actualPercent > 0 && (
          <div
            className="absolute inset-y-0 left-0 rounded-full bg-primary"
            style={{ width: `${actualPercent}%` }}
          />
        )}
        {/* Reserved marker */}
        {reservedPercent != null && (
          <div
            className="absolute top-1/2 h-3 w-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/60"
            style={{ left: `${reservedPercent}%` }}
          />
        )}
        {/* Limit marker */}
        {limitPercent != null && (
          <div
            className="absolute top-1/2 h-3 w-0.5 -translate-y-1/2 rounded-full bg-destructive"
            style={{ left: `${limitPercent}%` }}
          />
        )}
      </div>
    </div>
  );
}
