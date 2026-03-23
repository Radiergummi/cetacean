import { api } from "@/api/client";
import type { ServiceConfigRef } from "@/api/types";
import { EditableTable } from "@/components/EditableTable";
import ResourceName from "@/components/ResourceName";
import SimpleTable from "@/components/SimpleTable";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { splitStackPrefix } from "@/lib/searchConstants";
import { useCallback, useState } from "react";
import { Link } from "react-router-dom";

interface ConfigsEditorProps {
  serviceId: string;
  configs: ServiceConfigRef[];
  onSaved: (configs: ServiceConfigRef[]) => void;
}

export function ConfigsEditor({ serviceId, configs, onSaved }: ConfigsEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [availableConfigs, setAvailableConfigs] = useState<ComboboxOption[]>([]);
  const [newConfigId, setNewConfigId] = useState("");
  const [newTargetPath, setNewTargetPath] = useState("");

  const fetchConfigs = useCallback(() => {
    api
      .configs({ limit: 0 })
      .then((response) => {
        setAvailableConfigs(
          response.items.map((config) => ({
            value: config.ID,
            label: config.Spec.Name,
            description: config.ID.slice(0, 12),
          })),
        );
      })
      .catch(() => {});
  }, []);

  function handleConfigSelected(configId: string) {
    setNewConfigId(configId);

    const match = availableConfigs.find((option) => option.value === configId);

    if (match) {
      setNewTargetPath(`/${splitStackPrefix(match.label).name}`);
    }
  }

  return (
    <EditableTable<ServiceConfigRef>
      title="Configs"
      items={configs}
      columns={["Config", "Target"]}
      defaultOpen={configs.length > 0}
      editDisabled={!canEdit}
      onEditStart={fetchConfigs}
      emptyLabel="No configs attached"
      emptyHint="Click Edit to attach Docker configs to this service."
      keyFn={({ configID }) => configID}
      renderReadOnly={(items) => (
        <SimpleTable
          columns={["Name", "Target"]}
          items={items}
          keyFn={({ configID }) => configID}
          renderRow={({ configID, configName, fileName }) => (
            <>
              <td className="p-3 text-sm">
                <Link
                  to={`/configs/${configID}`}
                  className="text-link hover:underline"
                >
                  <ResourceName name={configName} />
                </Link>
              </td>
              <td className="p-3 font-mono text-sm">{fileName}</td>
            </>
          )}
        />
      )}
      renderKeyCell={({ configName }) => (
        <span className="text-xs">
          <ResourceName name={configName} />
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
          value={newConfigId}
          onChange={handleConfigSelected}
          options={availableConfigs}
          placeholder="Select config..."
          allowCustom={false}
        />
      )}
      renderAddValueCell={() => (
        <Input
          value={newTargetPath}
          onChange={(event) => setNewTargetPath(event.target.value)}
          placeholder="/my-config"
        />
      )}
      canAdd={!!newConfigId && !!newTargetPath}
      onAddCommit={() => {
        if (!newConfigId || !newTargetPath) {
          return null;
        }

        const match = availableConfigs.find((option) => option.value === newConfigId);

        return {
          configID: newConfigId,
          configName: match?.label ?? newConfigId,
          fileName: newTargetPath,
        };
      }}
      onAddReset={() => {
        setNewConfigId("");
        setNewTargetPath("");
      }}
      onSave={async (items) => {
        const result = await api.patchServiceConfigs(serviceId, items);
        onSaved(result.configs);
      }}
    />
  );
}
