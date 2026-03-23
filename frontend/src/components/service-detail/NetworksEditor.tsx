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
import { Pencil, Plus, Trash2 } from "lucide-react";
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

  function addNetwork() {
    if (!newNetworkId) {
      return;
    }

    setDraft([
      ...draft,
      {
        target: newNetworkId,
        aliases: newAliases.length > 0 ? [...newAliases] : undefined,
      },
    ]);
    setNewNetworkId("");
    setNewAliases([]);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.patchServiceNetworks(serviceId, draft);
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
        <div className="space-y-3 rounded-lg border p-3">
          {draft.length > 0 && (
            <SimpleTable
              columns={["Network", "Aliases", ""]}
              items={draft}
              keyFn={(_, index) => index}
              renderRow={({ target, aliases }, index) => (
                <>
                  <td className="p-3 text-sm">{nameMap[target] || target}</td>
                  <td className="p-3 font-mono text-sm">{aliases?.join(", ") || "\u2014"}</td>
                  <td className="p-3 text-right">
                    <Button
                      variant="outline"
                      size="xs"
                      onClick={() => removeNetwork(index)}
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
              <label className="text-xs font-medium text-foreground">Network</label>

              <Combobox
                value={newNetworkId}
                onChange={setNewNetworkId}
                options={filteredOptions}
                placeholder="Select network..."
                allowCustom={false}
              />
            </div>

            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <label className="text-xs font-medium text-foreground">Aliases</label>

              <MultiCombobox
                values={newAliases}
                onChange={setNewAliases}
                options={[]}
                placeholder="Type alias and press Enter..."
              />
            </div>

            <Button
              variant="outline"
              size="sm"
              onClick={addNetwork}
              disabled={!newNetworkId}
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
