import { api } from "@/api/client";
import type { Placement } from "@/api/types";
import { PlacementPanel } from "@/components/service-detail/PlacementPanel";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

interface PlacementEditorProps {
  serviceId: string;
  placement: Placement | null;
  onSaved: () => void;
}

export function PlacementEditor({ serviceId, placement, onSaved }: PlacementEditorProps) {
  const { level } = useOperationsLevel();
  const canEdit = level >= 1;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

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
      const nonEmpty = constraints.filter((c) => c.trim() !== "");

      await api.putServicePlacement(serviceId, {
        Constraints: nonEmpty.length > 0 ? nonEmpty : undefined,
        Preferences: placement?.Preferences,
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

          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
            disabled={!canEdit}
            title={canEdit ? undefined : "Editing disabled by server configuration"}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        </div>

        <PlacementPanel placement={placement ?? { Constraints: [], Preferences: [] }} />
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Placement
      </h3>

      <div className="space-y-2">
        <label className="text-sm font-medium">Constraints</label>

        {constraints.map((constraint, index) => (
          <div key={index} className="flex items-center gap-2">
            <Input
              value={constraint}
              onChange={(event) => updateConstraint(index, event.target.value)}
              placeholder="node.role==manager"
              className="font-mono text-sm"
            />

            <Button
              variant="outline"
              size="xs"
              onClick={() => removeConstraint(index)}
            >
              <Trash2 className="size-3" />
            </Button>
          </div>
        ))}

        <Button variant="outline" size="sm" onClick={addConstraint}>
          <Plus className="size-3" />
          Add constraint
        </Button>
      </div>

      <div className="space-y-2">
        <label className="text-sm font-medium">Max replicas per node</label>

        <Input
          type="number"
          min={0}
          value={maxReplicas || ""}
          onChange={(event) => setMaxReplicas(Number(event.target.value) || 0)}
          placeholder="0 (unlimited)"
          className="w-32"
        />
      </div>

      {saveError && <p className="text-xs text-red-600">{saveError}</p>}

      <div className="flex gap-2">
        <Button size="sm" onClick={save} disabled={saving}>
          {saving && <Spinner className="size-3" />}
          Save
        </Button>

        <Button variant="outline" size="sm" onClick={cancelEdit} disabled={saving}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
