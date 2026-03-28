import { DockerDocsLink } from "./DockerDocsLink";
import { ResourceRangeSlider } from "./resource-range-slider";
import { api } from "@/api/client";
import type { ClusterCapacity, SizingRecommendation } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
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

  if (highestValue === 0) {
    return nodeCapacity;
  }

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
  resources: { Limits, Reservations },
  pids,
  allocation,
  onSaved,
  hints,
}: {
  serviceId: string;
  resources: ServiceResourceShape;
  pids?: number;
  allocation?: AllocationData;
  onSaved: (updated: ServiceResourceShape) => void;
  hints?: SizingRecommendation[];
}) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  useEscapeCancel(editing, () => cancelEdit());

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
  const [capacityError, setCapacityError] = useState(false);

  function openEdit(suggested?: { resource: string; value: number }) {
    const cpuReservation =
      Reservations?.NanoCPUs != null ? Reservations.NanoCPUs / 1e9 : undefined;
    const cpuLimit = Limits?.NanoCPUs != null ? Limits.NanoCPUs / 1e9 : undefined;
    const memReservation =
      Reservations?.MemoryBytes != null ? Reservations.MemoryBytes / (1024 * 1024) : undefined;
    const memLimit = Limits?.MemoryBytes != null ? Limits.MemoryBytes / (1024 * 1024) : undefined;

    if (suggested?.resource === "cpu") {
      const suggestedCores = suggested.value / 1e9;

      setCpu({ reservation: suggestedCores, limit: cpuLimit });
    } else {
      setCpu({ reservation: cpuReservation, limit: cpuLimit });
    }

    if (suggested?.resource === "memory") {
      const suggestedMB = suggested.value / (1024 * 1024);

      setMemory({ reservation: suggestedMB, limit: memLimit });
    } else {
      setMemory({ reservation: memReservation, limit: memLimit });
    }

    setCapacity(null);
    setCapacityError(false);
    api
      .clusterCapacity()
      .then(setCapacity)
      .catch(() => setCapacityError(true));
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

      if (cpu.limit !== undefined) {
        patch.Limits.NanoCPUs = Math.round(cpu.limit * 1e9);
      }

      if (memory.limit !== undefined) {
        patch.Limits.MemoryBytes = Math.round(memory.limit * 1024 * 1024);
      }
    }

    if (cpu.reservation !== undefined || memory.reservation !== undefined) {
      patch.Reservations = {};

      if (cpu.reservation !== undefined) {
        patch.Reservations.NanoCPUs = Math.round(cpu.reservation * 1e9);
      }

      if (memory.reservation !== undefined) {
        patch.Reservations.MemoryBytes = Math.round(memory.reservation * 1024 * 1024);
      }
    }

    // No-op: nothing changed
    if (
      patch.Limits?.NanoCPUs === Limits?.NanoCPUs &&
      patch.Limits?.MemoryBytes === Limits?.MemoryBytes &&
      patch.Reservations?.NanoCPUs === Reservations?.NanoCPUs &&
      patch.Reservations?.MemoryBytes === Reservations?.MemoryBytes
    ) {
      setEditing(false);

      return;
    }

    setSaving(true);
    setSaveError(null);

    try {
      const updated = await api.patchServiceResources(serviceId, patch);

      onSaved(updated);
      setEditing(false);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Save failed"));
    } finally {
      setSaving(false);
    }
  }

  const hasResources =
    Limits?.NanoCPUs != null ||
    Limits?.MemoryBytes != null ||
    Reservations?.NanoCPUs != null ||
    Reservations?.MemoryBytes != null;

  // Only show allocation bars when actual usage data is available (requires Prometheus).
  // Without actual data, the text grid is a better fit.
  const hasActualUsage = allocation?.cpuActual != null || allocation?.memActual != null;

  const header = (
    <div className="flex items-center justify-between">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Resources
      </h3>
      {!editing && canEdit && (
        <Button
          variant="outline"
          size="xs"
          onClick={() => openEdit()}
        >
          <Pencil className="size-3" />
          Edit
        </Button>
      )}
    </div>
  );

  if (!editing) {
    return (
      <div className="space-y-3">
        {header}
        {hints && hints.length > 0 && (
          <div
            className={`rounded-md border p-3 text-sm ${
              hints.some(({ severity }) => severity === "critical")
                ? "border-red-200 bg-red-50 text-red-800 dark:border-red-800 dark:bg-red-950/30 dark:text-red-300"
                : hints.some(({ severity }) => severity === "warning")
                  ? "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-300"
                  : "border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-800 dark:bg-blue-950/30 dark:text-blue-300"
            }`}
          >
            <ul className="space-y-1">
              {hints.map((hint, index) => (
                <li
                  key={index}
                  className="flex items-center justify-between gap-2"
                >
                  <span>
                    {hint.message}
                    {hint.suggested != null &&
                      ` — consider ${
                        hint.resource === "memory"
                          ? formatBytes(hint.suggested)
                          : formatCores(hint.suggested / 1e9)
                      }`}
                  </span>
                  {hint.suggested != null && canEdit && (
                    <button
                      type="button"
                      className="shrink-0 text-xs font-medium underline"
                      onClick={() =>
                        openEdit({ resource: hint.resource, value: hint.suggested! })
                      }
                    >
                      Apply
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
        {!hasResources && pids == null ? (
          <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
            <p className="text-sm">No resource limits configured</p>
            {canEdit && (
              <p className="text-xs">Click Edit to set CPU and memory reservations and limits.</p>
            )}
          </div>
        ) : (
          <>
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
                {Limits?.NanoCPUs != null && (
                  <div>
                    <div className="text-xs text-muted-foreground">CPU Limit</div>
                    <div className="font-mono">{formatCores(Limits.NanoCPUs / 1e9)}</div>
                  </div>
                )}
                {Limits?.MemoryBytes != null && (
                  <div>
                    <div className="text-xs text-muted-foreground">Memory Limit</div>
                    <div className="font-mono">{formatBytes(Limits.MemoryBytes)}</div>
                  </div>
                )}
                {Reservations?.NanoCPUs != null && (
                  <div>
                    <div className="text-xs text-muted-foreground">CPU Reserved</div>
                    <div className="font-mono">{formatCores(Reservations.NanoCPUs / 1e9)}</div>
                  </div>
                )}
                {Reservations?.MemoryBytes != null && (
                  <div>
                    <div className="text-xs text-muted-foreground">Memory Reserved</div>
                    <div className="font-mono">{formatBytes(Reservations.MemoryBytes)}</div>
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
      {header}
      {capacity ? (
        <div className="space-y-3">
          <ResourceRangeSlider
            label={
              <span className="flex items-center gap-1">
                CPU (cores){" "}
                <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#reserve-memory" />
              </span>
            }
            reservation={cpu.reservation}
            limit={cpu.limit}
            onChange={setCpu}
            max={sliderMax(cpu.reservation, cpu.limit, capacity.maxNodeCPU, 0.25)}
            step={0.25}
            formatLabel={formatCpuTick}
          />
          <ResourceRangeSlider
            label={
              <span className="flex items-center gap-1">
                Memory{" "}
                <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#reserve-memory" />
              </span>
            }
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
      ) : capacityError ? (
        <p className="text-xs text-red-600 dark:text-red-400">
          Failed to load cluster capacity. Try closing and reopening the editor.
        </p>
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

  if (maxValue === 0) {
    return null;
  }

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
        {actualPercent > 0 && (
          <div
            className="absolute inset-y-0 left-0 rounded-full bg-primary"
            style={{ width: `${actualPercent}%` }}
          />
        )}
        {reservedPercent != null && (
          <div
            className="absolute top-1/2 h-3 w-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/60"
            style={{ left: `${reservedPercent}%` }}
          />
        )}
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
