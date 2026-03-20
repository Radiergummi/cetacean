import { api } from "@/api/client";
import type { PatchOp } from "@/api/types";
import { KeyValueEditor } from "@/components/KeyValueEditor";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import { handleCopyWithTemplates, renderSwarmTemplate } from "@/lib/swarmTemplates";

export function EnvEditor({
  serviceId,
  envVars,
  onSaved,
}: {
  serviceId: string;
  envVars: Record<string, string>;
  onSaved: (updated: Record<string, string>) => void;
}) {
  const { level } = useOperationsLevel();
  const canEdit = level >= 1;

  async function handleSave(operations: PatchOp[]) {
    const updated = await api.patchServiceEnv(serviceId, operations);

    onSaved(updated);

    return updated;
  }

  return (
    <KeyValueEditor
      title="Environment Variables"
      entries={envVars}
      keyLabel="Variable"
      valueLabel="Value"
      keyPlaceholder="NEW_VAR"
      valuePlaceholder="value"
      onSave={handleSave}
      renderValue={renderSwarmTemplate}
      onCopyValue={handleCopyWithTemplates}
      editDisabled={!canEdit}
      editDisabledTitle="Editing disabled by server configuration"
    />
  );
}
