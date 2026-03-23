import { api } from "@/api/client";
import type { ServiceNetworkRef } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import SimpleTable from "@/components/SimpleTable";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { MultiCombobox } from "@/components/ui/multi-combobox";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

interface NetworksEditorProps {
  serviceId: string;
  networks: ServiceNetworkRef[];
  networkNames: Record<string, string>;
  onSaved: (networks: ServiceNetworkRef[]) => void;
}

interface NetworkOption {
  value: string;
  label: string;
  description?: string;
}

export function NetworksEditor({
  serviceId,
  networks,
  networkNames,
  onSaved,
}: NetworksEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [draft, setDraft] = useState<ServiceNetworkRef[]>([]);
  const [availableNetworks, setAvailableNetworks] = useState<NetworkOption[]>([]);
  const [adding, setAdding] = useState(false);

  const [newNetworkId, setNewNetworkId] = useState("");
  const [newAliases, setNewAliases] = useState<string[]>([]);

  useEscapeCancel(editing, () => cancelEdit());

  /**
   * Build a merged name lookup from props + fetched networks,
   * so draft rows always have a displayable name.
   */
  const nameMap = useMemo(() => {
    const map: Record<string, string> = { ...networkNames };

    for (const option of availableNetworks) {
      if (!map[option.value]) {
        map[option.value] = option.label;
      }
    }

    return map;
  }, [networkNames, availableNetworks]);

  function openEdit() {
    setDraft(
      networks.map((network) => ({
        ...network,
        aliases: network.aliases ? [...network.aliases] : undefined,
      })),
    );
    setSaveError(null);
    setNewNetworkId("");
    setNewAliases([]);
    setAdding(networks.length === 0);
    setEditing(true);
  }

  useEffect(() => {
    if (!editing) {
      return;
    }

    let cancelled = false;

    api
      .networks({ limit: 0 })
      .then((response) => {
        if (!cancelled) {
          setAvailableNetworks(
            response.items.map((network) => ({
              value: network.Id,
              label: network.Name,
              description: network.Id.slice(0, 12),
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

  function removeNetwork(index: number) {
    setDraft(draft.filter((_, i) => i !== index));
  }

  function addRow() {
    if (!newNetworkId) {
      return;
    }

    setDraft((previous) => [
      ...previous,
      {
        target: newNetworkId,
        aliases: newAliases.length > 0 ? [...newAliases] : undefined,
      },
    ]);
    setNewNetworkId("");
    setNewAliases([]);
    setAdding(false);
  }

  async function save() {
    const effectiveDraft = newNetworkId
      ? [
          ...draft,
          {
            target: newNetworkId,
            aliases: newAliases.length > 0 ? [...newAliases] : undefined,
          },
        ]
      : draft;

    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.patchServiceNetworks(serviceId, effectiveDraft);
      setEditing(false);
      onSaved(result.networks);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update networks"));
    } finally {
      setSaving(false);
    }
  }

  const filteredOptions = useMemo(() => {
    const draftIds = new Set(draft.map(({ target }) => target));
    return availableNetworks.filter((option) => !draftIds.has(option.value));
  }, [draft, availableNetworks]);

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
      title="Networks"
      defaultOpen={networks.length > 0}
      controls={controls}
    >
      {editing ? (
        <div className="flex flex-col gap-3">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-max whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-background/50">
                <tr className="bg-muted/50 dark:bg-transparent">
                  <th className="ps-3 pt-3 text-left text-sm font-medium">Network</th>
                  <th className="ps-3 pt-3 text-left text-sm font-medium">Aliases</th>
                  <th className="w-12 py-3 ps-3" />
                </tr>
              </thead>
              <tbody>
                {draft.map(({ target, aliases }, index) => (
                  <tr
                    key={target}
                    className="border-b bg-transparent! last:border-b-0"
                  >
                    <td className="py-3 ps-3 font-mono text-xs">{nameMap[target] || target}</td>
                    <td className="py-3 ps-3">
                      <MultiCombobox
                        values={aliases ?? []}
                        onChange={(values) =>
                          setDraft((previous) =>
                            previous.map((item, i) =>
                              i === index
                                ? { ...item, aliases: values.length > 0 ? values : undefined }
                                : item,
                            ),
                          )
                        }
                        options={[]}
                        placeholder="Type alias and press Enter..."
                      />
                    </td>
                    <td className="py-3 ps-3">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => removeNetwork(index)}
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
                        value={newNetworkId}
                        onChange={setNewNetworkId}
                        options={filteredOptions}
                        placeholder="Select network..."
                        allowCustom={false}
                      />
                    </td>
                    <td className="py-3 ps-3">
                      <MultiCombobox
                        values={newAliases}
                        onChange={setNewAliases}
                        options={[]}
                        placeholder="Type alias and press Enter..."
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
                  if (adding && newNetworkId) addRow();
                  setAdding(true);
                }}
                disabled={adding && !newNetworkId}
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
      ) : networks.length === 0 ? (
        <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
          <p className="text-sm">No networks attached</p>
          {canEdit && (
            <p className="text-xs">Click Edit to attach Docker networks to this service.</p>
          )}
        </div>
      ) : (
        <SimpleTable
          columns={["Network", "Aliases"]}
          items={networks}
          keyFn={({ target }) => target}
          renderRow={({ target, aliases }) => (
            <>
              <td className="p-3 text-sm">
                <Link
                  to={`/networks/${target}`}
                  className="text-link hover:underline"
                >
                  {networkNames[target] || target}
                </Link>
              </td>
              <td className="p-3 font-mono text-sm">{aliases?.join(", ") || "\u2014"}</td>
            </>
          )}
        />
      )}
    </CollapsibleSection>
  );
}
