import { api } from "@/api/client";
import type { PatchOp } from "@/api/types";
import { KeyValueEditor } from "@/components/KeyValueEditor";

export function EnvEditor({
  serviceId,
  envVars,
  onSaved,
}: {
  serviceId: string;
  envVars: Record<string, string>;
  onSaved: (updated: Record<string, string>) => void;
}) {
  async function handleSave(ops: PatchOp[]) {
    const updated = await api.patchServiceEnv(serviceId, ops);
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
    />
  );
}
