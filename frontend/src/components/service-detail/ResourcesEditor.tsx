import CollapsibleSection from "../CollapsibleSection";
import { Spinner } from "../Spinner";
import { api } from "@/api/client";
import type { ClusterCapacity } from "@/api/types";
import { Button } from "@/components/ui/button";
import { SliderNumberField } from "@/components/ui/slider-number-field";
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

  const [limitCpuCores, setLimitCpuCores] = useState("");
  const [limitMemoryMegabytes, setLimitMemoryMegabytes] = useState("");
  const [reservedCpuCores, setReservedCpuCores] = useState("");
  const [reservedMemoryMegabytes, setReservedMemoryMegabytes] = useState("");
  const [capacity, setCapacity] = useState<ClusterCapacity | null>(null);

  function openEdit() {
    setLimitCpuCores(
      typed.limits?.nanoCPUs != null ? String(typed.limits.nanoCPUs / 1e9) : "",
    );
    setLimitMemoryMegabytes(
      typed.limits?.memoryBytes != null
        ? String(typed.limits.memoryBytes / (1024 * 1024))
        : "",
    );
    setReservedCpuCores(
      typed.reservations?.nanoCPUs != null
        ? String(typed.reservations.nanoCPUs / 1e9)
        : "",
    );
    setReservedMemoryMegabytes(
      typed.reservations?.memoryBytes != null
        ? String(typed.reservations.memoryBytes / (1024 * 1024))
        : "",
    );
    api.clusterCapacity().then(setCapacity).catch(() => {});
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  async function save() {
    const patch: ServiceResourceShape = {};
    if (limitCpuCores || limitMemoryMegabytes) {
      patch.limits = {};
      if (limitCpuCores) {
        patch.limits.nanoCPUs = Math.round(parseFloat(limitCpuCores) * 1e9);
      }
      if (limitMemoryMegabytes) {
        patch.limits.memoryBytes = Math.round(parseFloat(limitMemoryMegabytes) * 1024 * 1024);
      }
    }
    if (reservedCpuCores || reservedMemoryMegabytes) {
      patch.reservations = {};
      if (reservedCpuCores) {
        patch.reservations.nanoCPUs = Math.round(parseFloat(reservedCpuCores) * 1e9);
      }
      if (reservedMemoryMegabytes) {
        patch.reservations.memoryBytes = Math.round(
          parseFloat(reservedMemoryMegabytes) * 1024 * 1024,
        );
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
    <Button variant="outline" size="xs" onClick={openEdit}>
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
              <SliderNumberField
                label="CPU (cores)"
                value={limitCpuCores ? parseFloat(limitCpuCores) : undefined}
                onChange={(value) => setLimitCpuCores(value !== undefined ? String(value) : "")}
                min={0}
                max={capacity?.maxNodeCPU}
                step={0.25}
              />
              <SliderNumberField
                label="Memory (MB)"
                value={limitMemoryMegabytes ? parseFloat(limitMemoryMegabytes) : undefined}
                onChange={(value) =>
                  setLimitMemoryMegabytes(value !== undefined ? String(value) : "")
                }
                min={0}
                max={capacity ? capacity.maxNodeMemory / (1024 * 1024) : undefined}
                step={16}
              />
            </div>
            <div className="space-y-2 rounded-lg border p-3">
              <h4 className="text-xs font-medium text-muted-foreground uppercase">Reservations</h4>
              <SliderNumberField
                label="CPU (cores)"
                value={reservedCpuCores ? parseFloat(reservedCpuCores) : undefined}
                onChange={(value) => setReservedCpuCores(value !== undefined ? String(value) : "")}
                min={0}
                max={capacity?.maxNodeCPU}
                step={0.25}
              />
              <SliderNumberField
                label="Memory (MB)"
                value={reservedMemoryMegabytes ? parseFloat(reservedMemoryMegabytes) : undefined}
                onChange={(value) =>
                  setReservedMemoryMegabytes(value !== undefined ? String(value) : "")
                }
                min={0}
                max={capacity ? capacity.maxNodeMemory / (1024 * 1024) : undefined}
                step={16}
              />
            </div>
          </div>
          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}
          <div className="flex gap-2">
            <Button size="xs" onClick={() => void save()} disabled={saving}>
              {saving && <Spinner className="size-3" />}
              Save
            </Button>
            <Button variant="outline" size="xs" onClick={cancelEdit} disabled={saving}>
              <X className="size-3" />
              Cancel
            </Button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}
