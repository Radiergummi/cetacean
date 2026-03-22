import { api } from "@/api/client";
import type { PatchOp } from "@/api/types";
import { KeyValueEditor } from "@/components/KeyValueEditor";
import { DockerDocsLink } from "@/components/service-detail/DockerDocsLink";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
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
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  async function handleSave(operations: PatchOp[]) {
    const updated = await api.patchServiceEnv(serviceId, operations);

    onSaved(updated);

    return updated;
  }

  return (
    <KeyValueEditor
      title="Environment Variables"
      titleExtra={
        <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#env" />
      }
      entries={envVars}
      defaultOpen={Object.keys(envVars).length > 0}
      keyLabel="Variable"
      valueLabel="Value"
      keyPlaceholder="NEW_VAR"
      valuePlaceholder="value"
      onSave={handleSave}
      renderValue={renderSwarmTemplate}
      onCopyValue={handleCopyWithTemplates}
      editDisabled={!canEdit}
    />
  );
}
