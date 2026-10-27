import { api } from "@/api/client";
import type { ServiceConfigRef } from "@/api/types";
import { EditableTable } from "@/components/EditableTable";
import ResourceName from "@/components/ResourceName";
import SimpleTable from "@/components/SimpleTable";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { useEditVersion } from "@/hooks/useEditVersion";
import { splitStackPrefix } from "@/lib/searchConstants";
import { useCallback, useState } from "react";
import { Link } from "react-router-dom";

interface ConfigsEditorProps {
  serviceId: string;
  configs: ServiceConfigRef[];
  onSaved: (configs: ServiceConfigRef[]) => void;
}

/**
 * The target a newly attached config defaults to: the path a removed config
 * vacated in this edit, so a rotation keeps the file the app reads.
 */
export function defaultConfigTarget(
  configName: string,
  saved: ServiceConfigRef[],
  draft: ServiceConfigRef[],
): string {
  const vacated = saved.find(({ fileName }) => !draft.some((item) => item.fileName === fileName));

  return vacated?.fileName ?? `/${splitStackPrefix(configName).name}`;
}

export function ConfigsEditor({
  serviceId,
  configs,
  onSaved,
  canEdit = false,
}: ConfigsEditorProps & { canEdit?: boolean }) {
  const version = useEditVersion(`/services/${serviceId}/configs`);
  const [availableConfigs, setAvailableConfigs] = useState<ComboboxOption[]>([]);
  const [newConfigId, setNewConfigId] = useState("");
  const [newTargetPath, setNewTargetPath] = useState("");

  const fetchConfigs = useCallback(() => {
    api
      .allConfigs()
      .then((configs) => {
        setAvailableConfigs(
          configs.map((config) => ({
            value: config.ID,
            label: config.Spec.Name,
            description: config.ID.slice(0, 12),
          })),
        );
      })
      .catch(console.warn);
  }, []);

  function handleConfigSelected(configId: string, draft: ServiceConfigRef[]) {
    setNewConfigId(configId);

    const match = availableConfigs.find((option) => option.value === configId);

    if (match) {
      setNewTargetPath(defaultConfigTarget(match.label, configs, draft));
    }
  }

  return (
    <EditableTable<ServiceConfigRef>
      title="Configs"
      items={configs}
      columns={["Config", "Target"]}
      defaultOpen={configs.length > 0}
      editDisabled={!canEdit}
      onEditStart={() => {
        version.capture();
        fetchConfigs();
      }}
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
      renderAddKeyCell={(draft) => (
        <Combobox
          value={newConfigId}
          onChange={(configId) => handleConfigSelected(configId, draft)}
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
        const result = await api.patchServiceConfigs(serviceId, items, await version.ifMatch());
        onSaved(result.configs);
      }}
    />
  );
}
