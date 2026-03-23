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
import { Pencil, Trash2 } from "lucide-react";
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
  description?: string;
}

export function ConfigsEditor({ serviceId, configs, onSaved }: ConfigsEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [draft, setDraft] = useState<ServiceConfigRef[]>([]);
  const [availableConfigs, setAvailableConfigs] = useState<ConfigOption[]>([]);
  const [adding, setAdding] = useState(false);

  const [newConfigId, setNewConfigId] = useState("");
  const [newTargetPath, setNewTargetPath] = useState("");

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setDraft(configs.map((config) => ({ ...config })));
    setSaveError(null);
    setNewConfigId("");
    setNewTargetPath("");
    setAdding(configs.length === 0);
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

  function addRow() {
    if (!newConfigId || !newTargetPath) {
      return;
    }

    const match = availableConfigs.find((option) => option.value === newConfigId);
    const configName = match ? match.label : newConfigId;

    setDraft((previous) => [
      ...previous,
      { configID: newConfigId, configName, fileName: newTargetPath },
    ]);
    setNewConfigId("");
    setNewTargetPath("");
    setAdding(false);
  }

  async function save() {
    const effectiveDraft =
      newConfigId && newTargetPath
        ? [
            ...draft,
            {
              configID: newConfigId,
              configName:
                availableConfigs.find((option) => option.value === newConfigId)?.label ??
                newConfigId,
              fileName: newTargetPath,
            },
          ]
        : draft;

    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.patchServiceConfigs(serviceId, effectiveDraft);
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
        <div className="flex flex-col gap-3">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-max whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-background/50">
                <tr className="bg-muted/50 dark:bg-transparent">
                  <th className="ps-3 pt-3 text-left text-sm font-medium">Config</th>
                  <th className="ps-3 pt-3 text-left text-sm font-medium">Target</th>
                  <th className="w-12 py-3 ps-3" />
                </tr>
              </thead>
              <tbody>
                {draft.map(({ configID, configName, fileName }, index) => (
                  <tr
                    key={configID}
                    className="border-b bg-transparent! last:border-b-0"
                  >
                    <td className="py-3 ps-3 font-mono text-xs">{configName}</td>
                    <td className="py-3 ps-3">
                      <Input
                        value={fileName}
                        onChange={(event) =>
                          setDraft((previous) =>
                            previous.map((item, i) =>
                              i === index ? { ...item, fileName: event.target.value } : item,
                            ),
                          )
                        }
                        className="font-mono text-xs"
                      />
                    </td>
                    <td className="py-3 ps-3">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => removeConfig(index)}
                        title="Remove"
                        className="text-muted-foreground hover:text-red-600"
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </td>
                  </tr>
                ))}
                {adding && (
                  <tr className="bg-transparent!">
                    <td className="py-3 ps-3">
                      <Combobox
                        value={newConfigId}
                        onChange={handleConfigSelected}
                        options={availableConfigs}
                        placeholder="Select config..."
                        allowCustom={false}
                      />
                    </td>
                    <td className="py-3 ps-3">
                      <Input
                        value={newTargetPath}
                        onChange={(event) => setNewTargetPath(event.target.value)}
                        placeholder="/my-config"
                        onKeyDown={(event) => {
                          if (event.key === "Enter" && newConfigId && newTargetPath) {
                            addRow();
                          }
                        }}
                      />
                    </td>
                    <td />
                  </tr>
                )}
              </tbody>
            </table>

            {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

            <footer className="flex items-center gap-2 p-3">
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  if (adding && newConfigId && newTargetPath) addRow();
                  setAdding(true);
                }}
                disabled={adding && (!newConfigId || !newTargetPath)}
              >
                Add another
              </Button>
              <div className="ml-auto flex gap-2">
                <Button
                  size="sm"
                  onClick={() => void save()}
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
