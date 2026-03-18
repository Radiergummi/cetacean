import { api } from "@/api/client";
import type { ClusterCapacity } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { SliderNumberField } from "@/components/ui/slider-number-field";
import { formatBytes, formatCores } from "@/lib/format";
import { Pencil, X } from "lucide-react";
import { useRef, useState } from "react";

export interface ServiceResourceShape {
  limits?: { nanoCPUs?: number; memoryBytes?: number; pids?: number };
  reservations?: { nanoCPUs?: number; memoryBytes?: number };
}

export function ResourcesEditor({
  serviceId,
  resources,
  onSaved,
}: {
  serviceId: string;
  resources: ServiceResourceShape;
  onSaved: (updated: Record<string, unknown>) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const [limitCpuCores, setLimitCpuCores] = useState<number | undefined>();
  const [limitMemoryMegabytes, setLimitMemoryMegabytes] = useState<number | undefined>();
  const [reservedCpuCores, setReservedCpuCores] = useState<number | undefined>();
  const [reservedMemoryMegabytes, setReservedMemoryMegabytes] = useState<number | undefined>();
  const [capacity, setCapacity] = useState<ClusterCapacity | null>(null);
  const capacityFetched = useRef(false);

  function openEdit() {
    setLimitCpuCores(
      resources.limits?.nanoCPUs != null ? resources.limits.nanoCPUs / 1e9 : undefined,
    );
    setLimitMemoryMegabytes(
      resources.limits?.memoryBytes != null
        ? resources.limits.memoryBytes / (1024 * 1024)
        : undefined,
    );
    setReservedCpuCores(
      resources.reservations?.nanoCPUs != null ? resources.reservations.nanoCPUs / 1e9 : undefined,
    );
    setReservedMemoryMegabytes(
      resources.reservations?.memoryBytes != null
        ? resources.reservations.memoryBytes / (1024 * 1024)
        : undefined,
    );
    if (!capacityFetched.current) {
      capacityFetched.current = true;
      api
        .clusterCapacity()
        .then(setCapacity)
        .catch(() => {});
    }
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  async function save() {
    const patch: ServiceResourceShape = {};
    if (limitCpuCores !== undefined || limitMemoryMegabytes !== undefined) {
      patch.limits = {};
      if (limitCpuCores !== undefined) {
        patch.limits.nanoCPUs = Math.round(limitCpuCores * 1e9);
      }
      if (limitMemoryMegabytes !== undefined) {
        patch.limits.memoryBytes = Math.round(limitMemoryMegabytes * 1024 * 1024);
      }
    }
    if (reservedCpuCores !== undefined || reservedMemoryMegabytes !== undefined) {
      patch.reservations = {};
      if (reservedCpuCores !== undefined) {
        patch.reservations.nanoCPUs = Math.round(reservedCpuCores * 1e9);
      }
      if (reservedMemoryMegabytes !== undefined) {
        patch.reservations.memoryBytes = Math.round(reservedMemoryMegabytes * 1024 * 1024);
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
    resources.limits?.nanoCPUs ||
    resources.limits?.memoryBytes ||
    resources.reservations?.nanoCPUs ||
    resources.reservations?.memoryBytes;

  const controls = !editing ? (
    <Button
      variant="outline"
      size="xs"
      onClick={openEdit}
    >
      <Pencil className="size-3" />
      Edit
    </Button>
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
        )
      ) : (
        <div className="space-y-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border p-3">
              <h4 className="text-xs font-medium text-muted-foreground uppercase">Limits</h4>
              <SliderNumberField
                label="CPU (cores)"
                value={limitCpuCores}
                onChange={setLimitCpuCores}
                min={0}
                max={capacity?.maxNodeCPU}
                step={0.25}
              />
              <SliderNumberField
                label="Memory (MB)"
                value={limitMemoryMegabytes}
                onChange={setLimitMemoryMegabytes}
                min={0}
                max={capacity ? capacity.maxNodeMemory / (1024 * 1024) : undefined}
                step={16}
              />
            </div>
            <div className="space-y-2 rounded-lg border p-3">
              <h4 className="text-xs font-medium text-muted-foreground uppercase">Reservations</h4>
              <SliderNumberField
                label="CPU (cores)"
                value={reservedCpuCores}
                onChange={setReservedCpuCores}
                min={0}
                max={capacity?.maxNodeCPU}
                step={0.25}
              />
              <SliderNumberField
                label="Memory (MB)"
                value={reservedMemoryMegabytes}
                onChange={setReservedMemoryMegabytes}
                min={0}
                max={capacity ? capacity.maxNodeMemory / (1024 * 1024) : undefined}
                step={16}
              />
            </div>
          </div>
          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}
          <div className="flex gap-2">
            <Button
              size="xs"
              onClick={() => void save()}
              disabled={saving}
            >
              {saving && <Spinner className="size-3" />}
              Save
            </Button>
            <Button
              variant="outline"
              size="xs"
              onClick={cancelEdit}
              disabled={saving}
            >
              <X className="size-3" />
              Cancel
            </Button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}
