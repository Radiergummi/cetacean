import { api } from "@/api/client";
import type { Placement } from "@/api/types";
import { PlacementPanel } from "@/components/service-detail/PlacementPanel";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { SliderNumberField } from "@/components/ui/slider-number-field";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

interface PlacementEditorProps {
  serviceId: string;
  placement: Placement | null;
  onSaved: () => void;
}

export function PlacementEditor({ serviceId, placement, onSaved }: PlacementEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  useEscapeCancel(editing, () => cancelEdit());

  const [constraints, setConstraints] = useState<string[]>([]);
  const [maxReplicas, setMaxReplicas] = useState<number>(0);

  function openEdit() {
    setConstraints([...(placement?.Constraints ?? [])]);
    setMaxReplicas(placement?.MaxReplicas ?? 0);
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addConstraint() {
    setConstraints([...constraints, ""]);
  }

  function removeConstraint(index: number) {
    setConstraints(constraints.filter((_, i) => i !== index));
  }

  function updateConstraint(index: number, value: string) {
    const updated = [...constraints];
    updated[index] = value;
    setConstraints(updated);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const nonEmpty = constraints.filter((constraint) => constraint.trim() !== "");

      // Fetch fresh placement to avoid overwriting externally changed Preferences
      const current = await api.servicePlacement(serviceId);

      await api.putServicePlacement(serviceId, {
        Constraints: nonEmpty.length > 0 ? nonEmpty : undefined,
        Preferences: current?.Preferences,
        MaxReplicas: maxReplicas || undefined,
      });

      setEditing(false);
      onSaved();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update placement"));
    } finally {
      setSaving(false);
    }
  }

  if (!editing) {
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Placement
          </h3>

          {canEdit && (
            <Button
              variant="outline"
              size="xs"
              onClick={openEdit}
            >
              <Pencil className="size-3" />
              Edit
            </Button>
          )}
        </div>

        <PlacementPanel
          placement={placement ?? { Constraints: [], Preferences: [] }}
          canEdit={canEdit}
        />
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Placement
      </h3>

      <div className="w-48">
        <SliderNumberField
          label="Max replicas per node"
          value={maxReplicas || undefined}
          onChange={(value) => setMaxReplicas(value ?? 0)}
          min={0}
          step={1}
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-muted-foreground">Constraints</label>

        {constraints.map((constraint, index) => (
          <div
            key={index}
            className="flex items-center gap-2"
          >
            <input
              value={constraint}
              onChange={(event) => updateConstraint(index, event.target.value)}
              placeholder="node.role==manager"
              className="h-8 w-full rounded-md border border-input bg-transparent px-3 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />

            <Button
              variant="outline"
              size="xs"
              className="h-8 shrink-0"
              onClick={() => removeConstraint(index)}
            >
              <Trash2 className="size-3" />
            </Button>
          </div>
        ))}
      </div>

      {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

      <footer className="flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={addConstraint}
        >
          <Plus className="size-3" />
          Add constraint
        </Button>

        <div className="ml-auto flex gap-2">
          <Button
            size="sm"
            onClick={save}
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
        </div>
      </footer>
    </div>
  );
}
