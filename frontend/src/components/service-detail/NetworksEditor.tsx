import { api } from "@/api/client";
import type { ServiceNetworkRef } from "@/api/types";
import { EditableTable } from "@/components/EditableTable";
import ResourceName from "@/components/ResourceName";
import SimpleTable from "@/components/SimpleTable";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { MultiCombobox } from "@/components/ui/multi-combobox";
import { useCallback, useMemo, useState } from "react";
import { Link } from "react-router-dom";

interface NetworksEditorProps {
  serviceId: string;
  networks: ServiceNetworkRef[];
  networkNames: Record<string, string>;
  onSaved: (networks: ServiceNetworkRef[]) => void;
}

export function NetworksEditor({
  serviceId,
  networks,
  networkNames,
  onSaved,
  canEdit = false,
}: NetworksEditorProps & { canEdit?: boolean }) {
  const [availableNetworks, setAvailableNetworks] = useState<ComboboxOption[]>([]);
  const [newNetworkId, setNewNetworkId] = useState("");
  const [newAliases, setNewAliases] = useState<string[]>([]);

  const fetchNetworks = useCallback(() => {
    api
      .networks()
      .then(({ data: response }) => {
        setAvailableNetworks(
          response.items.map((network) => ({
            value: network.Id,
            label: network.Name,
            description: network.Id.slice(0, 12),
          })),
        );
      })
      .catch(console.warn);
  }, []);

  /**
   * Build a merged name lookup from props and fetched networks,
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

  return (
    <EditableTable<ServiceNetworkRef>
      title="Networks"
      items={networks}
      columns={["Network", "Aliases"]}
      defaultOpen={networks.length > 0}
      editDisabled={!canEdit}
      onEditStart={fetchNetworks}
      emptyLabel="No networks attached"
      emptyHint="Click Edit to attach Docker networks to this service."
      keyFn={({ target }) => target}
      renderReadOnly={(items) => (
        <SimpleTable
          columns={["Network", "Aliases"]}
          items={items}
          keyFn={({ target }) => target}
          renderRow={({ target, aliases }) => (
            <>
              <td className="p-3 text-sm">
                <Link
                  to={`/networks/${target}`}
                  className="text-link hover:underline"
                >
                  <ResourceName name={networkNames[target] || target} />
                </Link>
              </td>
              <td className="p-3 font-mono text-sm">{aliases?.join(", ") || "\u2014"}</td>
            </>
          )}
        />
      )}
      renderKeyCell={({ target }) => (
        <span className="text-xs">
          <ResourceName name={nameMap[target] || target} />
        </span>
      )}
      renderValueCell={(item, _index, update) => (
        <MultiCombobox
          values={item.aliases ?? []}
          onChange={(values) =>
            update({ ...item, aliases: values.length > 0 ? values : undefined })
          }
          options={[]}
          placeholder="Type alias and press Enter..."
        />
      )}
      renderAddKeyCell={(draft) => {
        const draftIds = new Set(draft.map(({ target }) => target));
        const filteredOptions = availableNetworks.filter((option) => !draftIds.has(option.value));

        return (
          <Combobox
            value={newNetworkId}
            onChange={setNewNetworkId}
            options={filteredOptions}
            placeholder="Select network..."
            allowCustom={false}
          />
        );
      }}
      renderAddValueCell={() => (
        <MultiCombobox
          values={newAliases}
          onChange={setNewAliases}
          options={[]}
          placeholder="Type alias and press Enter..."
        />
      )}
      canAdd={!!newNetworkId}
      onAddCommit={() => {
        if (!newNetworkId) {
          return null;
        }

        return {
          target: newNetworkId,
          aliases: newAliases.length > 0 ? [...newAliases] : undefined,
        };
      }}
      onAddReset={() => {
        setNewNetworkId("");
        setNewAliases([]);
      }}
      onSave={async (items) => {
        const result = await api.patchServiceNetworks(serviceId, items);
        onSaved(result.networks);
      }}
    />
  );
}
