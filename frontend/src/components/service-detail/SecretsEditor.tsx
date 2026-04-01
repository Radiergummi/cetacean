import { api } from "@/api/client";
import type { ServiceSecretRef } from "@/api/types";
import { EditableTable } from "@/components/EditableTable";
import ResourceName from "@/components/ResourceName";
import SimpleTable from "@/components/SimpleTable";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { splitStackPrefix } from "@/lib/searchConstants";
import { useCallback, useState } from "react";
import { Link } from "react-router-dom";

interface SecretsEditorProps {
  serviceId: string;
  secrets: ServiceSecretRef[];
  onSaved: (secrets: ServiceSecretRef[]) => void;
}

export function SecretsEditor({ serviceId, secrets, onSaved }: SecretsEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [availableSecrets, setAvailableSecrets] = useState<ComboboxOption[]>([]);
  const [newSecretId, setNewSecretId] = useState("");
  const [newTargetPath, setNewTargetPath] = useState("");

  const fetchSecrets = useCallback(() => {
    api
      .secrets()
      .then((response) => {
        setAvailableSecrets(
          response.items.map((secret) => ({
            value: secret.ID,
            label: secret.Spec.Name,
            description: secret.ID.slice(0, 12),
          })),
        );
      })
      .catch(console.warn);
  }, []);

  function handleSecretSelected(secretId: string) {
    setNewSecretId(secretId);

    const match = availableSecrets.find((option) => option.value === secretId);

    if (match) {
      setNewTargetPath(`/run/secrets/${splitStackPrefix(match.label).name}`);
    }
  }

  return (
    <EditableTable<ServiceSecretRef>
      title="Secrets"
      items={secrets}
      columns={["Secret", "Target"]}
      defaultOpen={secrets.length > 0}
      editDisabled={!canEdit}
      onEditStart={fetchSecrets}
      emptyLabel="No secrets attached"
      emptyHint="Click Edit to attach Docker secrets to this service."
      keyFn={({ secretID }) => secretID}
      renderReadOnly={(items) => (
        <SimpleTable
          columns={["Name", "Target"]}
          items={items}
          keyFn={({ secretID }) => secretID}
          renderRow={({ secretID, secretName, fileName }) => (
            <>
              <td className="p-3 text-sm">
                <Link
                  to={`/secrets/${secretID}`}
                  className="text-link hover:underline"
                >
                  <ResourceName name={secretName} />
                </Link>
              </td>
              <td className="p-3 font-mono text-sm">{fileName}</td>
            </>
          )}
        />
      )}
      renderKeyCell={({ secretName }) => (
        <span className="text-xs">
          <ResourceName name={secretName} />
        </span>
      )}
      renderValueCell={(item, _index, update) => (
        <Input
          value={item.fileName}
          onChange={(event) => update({ ...item, fileName: event.target.value })}
          className="font-mono text-xs"
        />
      )}
      renderAddKeyCell={() => (
        <Combobox
          value={newSecretId}
          onChange={handleSecretSelected}
          options={availableSecrets}
          placeholder="Select secret..."
          allowCustom={false}
        />
      )}
      renderAddValueCell={() => (
        <Input
          value={newTargetPath}
          onChange={(event) => setNewTargetPath(event.target.value)}
          placeholder="/run/secrets/my-secret"
        />
      )}
      canAdd={!!newSecretId && !!newTargetPath}
      onAddCommit={() => {
        if (!newSecretId || !newTargetPath) {
          return null;
        }

        const match = availableSecrets.find((option) => option.value === newSecretId);

        return {
          secretID: newSecretId,
          secretName: match?.label ?? newSecretId,
          fileName: newTargetPath,
        };
      }}
      onAddReset={() => {
        setNewSecretId("");
        setNewTargetPath("");
      }}
      onSave={async (items) => {
        const result = await api.patchServiceSecrets(serviceId, items);
        onSaved(result.secrets);
      }}
    />
  );
}
