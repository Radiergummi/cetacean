import { api } from "@/api/client";
import type { ServiceConfigRef } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import SimpleTable from "@/components/SimpleTable";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";

interface ConfigsEditorProps {
  serviceId: string;
  configs: ServiceConfigRef[];
  onSaved: (configs: ServiceConfigRef[]) => void;
}

interface ConfigOption {
  value: string;
  label: string;
}

export function ConfigsEditor({ serviceId, configs, onSaved }: ConfigsEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [draft, setDraft] = useState<ServiceConfigRef[]>([]);
  const [availableConfigs, setAvailableConfigs] = useState<ConfigOption[]>([]);

  const [newConfigId, setNewConfigId] = useState("");
  const [newTargetPath, setNewTargetPath] = useState("");

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setDraft(configs.map((config) => ({ ...config })));
    setSaveError(null);
    setNewConfigId("");
    setNewTargetPath("");
    setEditing(true);
  }

  useEffect(() => {
    if (!editing) {
      return;
    }

    let cancelled = false;

    api
      .configs({ limit: 0 })
      .then((response) => {
        if (!cancelled) {
          setAvailableConfigs(
            response.items.map((config) => ({
              value: config.ID,
              label: config.Spec.Name,
              description: config.ID.slice(0, 12),
            })),
          );
        }
      })
      .catch(() => {});

    return () => {
      cancelled = true;
    };
  }, [editing]);

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function removeConfig(index: number) {
    setDraft(draft.filter((_, i) => i !== index));
  }

  function handleConfigSelected(configId: string) {
    setNewConfigId(configId);

    const match = availableConfigs.find((option) => option.value === configId);

    if (match) {
      setNewTargetPath(`/${match.label}`);
    }
  }

  function addConfig() {
    if (!newConfigId || !newTargetPath) {
      return;
    }

    const match = availableConfigs.find((option) => option.value === newConfigId);
    const configName = match ? match.label : newConfigId;

    setDraft([...draft, { configID: newConfigId, configName, fileName: newTargetPath }]);
    setNewConfigId("");
    setNewTargetPath("");
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.patchServiceConfigs(serviceId, draft);
      setEditing(false);
      onSaved(result.configs);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update configs"));
    } finally {
      setSaving(false);
    }
  }

  const controls =
    !editing && canEdit ? (
      <Button
        variant="outline"
        size="xs"
        onClick={(event: React.MouseEvent) => {
          event.stopPropagation();
          openEdit();
        }}
      >
        <Pencil className="size-3" />
        Edit
      </Button>
    ) : undefined;

  return (
    <CollapsibleSection
      title="Configs"
      defaultOpen={configs.length > 0}
      controls={controls}
    >
      {editing ? (
        <div className="space-y-3 rounded-lg border p-3">
          {draft.length > 0 && (
            <SimpleTable
              columns={["Name", "Target", ""]}
              items={draft}
              keyFn={(_, index) => index}
              renderRow={({ configName, fileName }, index) => (
                <>
                  <td className="p-3 text-sm">{configName}</td>
                  <td className="p-3 font-mono text-sm">{fileName}</td>
                  <td className="p-3 text-right">
                    <Button
                      variant="outline"
                      size="xs"
                      onClick={() => removeConfig(index)}
                    >
                      <Trash2 className="size-3" />
                    </Button>
                  </td>
                </>
              )}
            />
          )}

          <div className="flex items-end gap-2 border-t border-dashed pt-3">
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <label className="text-xs font-medium text-foreground">Config</label>

              <Combobox
                value={newConfigId}
                onChange={handleConfigSelected}
                options={availableConfigs}
                placeholder="Select config..."
                allowCustom={false}
              />
            </div>

            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <label className="text-xs font-medium text-foreground">Target path</label>

              <Input
                value={newTargetPath}
                onChange={(event) => setNewTargetPath(event.target.value)}
                placeholder="/my-config"
              />
            </div>

            <Button
              variant="outline"
              size="sm"
              onClick={addConfig}
              disabled={!newConfigId || !newTargetPath}
            >
              <Plus className="size-3" />
              Add
            </Button>
          </div>

          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

          <footer className="flex items-center gap-2">
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
      ) : configs.length === 0 ? (
        <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
          <p className="text-sm">No configs attached</p>
          {canEdit && (
            <p className="text-xs">Click Edit to attach Docker configs to this service.</p>
          )}
        </div>
      ) : (
        <SimpleTable
          columns={["Name", "Target"]}
          items={configs}
          keyFn={({ configID }) => configID}
          renderRow={({ configID, configName, fileName }) => (
            <>
              <td className="p-3 text-sm">
                <Link
                  to={`/configs/${configID}`}
                  className="text-link hover:underline"
                >
                  {configName}
                </Link>
              </td>
              <td className="p-3 font-mono text-sm">{fileName}</td>
            </>
          )}
        />
      )}
    </CollapsibleSection>
  );
}
