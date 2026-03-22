import { api } from "@/api/client";
import type { Placement } from "@/api/types";
import { DockerDocsLink } from "@/components/service-detail/DockerDocsLink";
import { EditablePanel } from "@/components/service-detail/EditablePanel";
import { PlacementPanel } from "@/components/service-detail/PlacementPanel";
import { Button } from "@/components/ui/button";
import { SliderNumberField } from "@/components/ui/slider-number-field";
import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

interface PlacementEditorProps {
  serviceId: string;
  placement: Placement | null;
  onSaved: () => void;
}

export function PlacementEditor({ serviceId, placement, onSaved }: PlacementEditorProps) {
  const [constraints, setConstraints] = useState<string[]>([]);
  const [maxReplicas, setMaxReplicas] = useState<number>(0);

  function resetForm() {
    setConstraints([...(placement?.Constraints ?? [])]);
    setMaxReplicas(placement?.MaxReplicas ?? 0);
  }

  function updateConstraint(index: number, value: string) {
    const updated = [...constraints];
    updated[index] = value;
    setConstraints(updated);
  }

  async function save() {
    const nonEmpty = constraints.filter((constraint) => constraint.trim() !== "");

    // Fetch fresh placement to avoid overwriting externally changed Preferences
    const current = await api.servicePlacement(serviceId);

    await api.putServicePlacement(serviceId, {
      Constraints: nonEmpty.length > 0 ? nonEmpty : undefined,
      Preferences: current?.Preferences,
      MaxReplicas: maxReplicas || undefined,
    });

    onSaved();
  }

  return (
    <EditablePanel
      title="Placement"
      bordered={false}
      empty={
        !placement?.Constraints?.length &&
        !placement?.MaxReplicas &&
        !placement?.Preferences?.length
      }
      emptyDescription="Click Edit to control which nodes this service can run on."
      onOpen={resetForm}
      onSave={save}
      actions={
        <Button
          variant="outline"
          size="sm"
          onClick={() => setConstraints([...constraints, ""])}
        >
          <Plus className="size-3" />
          Add constraint
        </Button>
      }
      display={<PlacementPanel placement={placement ?? { Constraints: [], Preferences: [] }} />}
      edit={
        <>
          <div className="w-48">
            <SliderNumberField
              label={
                <span className="flex items-center gap-1">
                  Max replicas per node{" "}
                  <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#replicas-max-per-node" />
                </span>
              }
              value={maxReplicas || undefined}
              onChange={(value) => setMaxReplicas(value ?? 0)}
              min={0}
              step={1}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="flex items-center gap-1 text-xs font-medium text-foreground">
              Constraints{" "}
              <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#constraint" />
            </label>

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
                  onClick={() => setConstraints(constraints.filter((_, i) => i !== index))}
                >
                  <Trash2 className="size-3" />
                </Button>
              </div>
            ))}
          </div>
        </>
      }
    />
  );
}
