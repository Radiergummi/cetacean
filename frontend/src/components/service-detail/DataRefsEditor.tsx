import { api } from "@/api/client";
import { EditableTable } from "@/components/EditableTable";
import ResourceName from "@/components/ResourceName";
import SimpleTable from "@/components/SimpleTable";
import { Combobox, type ComboboxOption } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { useEditVersion } from "@/hooks/useEditVersion";
import { splitStackPrefix } from "@/lib/searchConstants";
import { useCallback, useState } from "react";
import { Link } from "react-router-dom";

/** A config or secret attached to a service, and the file it is mounted at. */
export interface DataRef {
  id: string;
  name: string;
  fileName: string;
}

type DataKind = "config" | "secret";

const kinds = {
  config: {
    title: "Configs",
    collection: "configs",
    targetDirectory: "/",
    placeholder: "/my-config",
    list: api.allConfigs,
    save: async (serviceId: string, refs: DataRef[], ifMatch: string | undefined) => {
      await api.patchServiceConfigs(
        serviceId,
        refs.map(({ id, name, fileName }) => ({ configID: id, configName: name, fileName })),
        ifMatch,
      );
    },
  },
  secret: {
    title: "Secrets",
    collection: "secrets",
    targetDirectory: "/run/secrets/",
    placeholder: "/run/secrets/my-secret",
    list: api.allSecrets,
    save: async (serviceId: string, refs: DataRef[], ifMatch: string | undefined) => {
      await api.patchServiceSecrets(
        serviceId,
        refs.map(({ id, name, fileName }) => ({ secretID: id, secretName: name, fileName })),
        ifMatch,
      );
    },
  },
} as const;

/**
 * The target a newly attached config or secret defaults to: the path a removed
 * one vacated in this edit, so a rotation keeps the file the app reads.
 */
export function defaultTarget(
  kind: DataKind,
  name: string,
  saved: DataRef[],
  draft: DataRef[],
): string {
  const vacated = saved.find(({ fileName }) => !draft.some((item) => item.fileName === fileName));

  return vacated?.fileName ?? `${kinds[kind].targetDirectory}${splitStackPrefix(name).name}`;
}

interface Props {
  kind: DataKind;
  serviceId: string;
  refs: DataRef[];
  onSaved: () => void;
  canEdit?: boolean | undefined;
}

export function DataRefsEditor({ kind, serviceId, refs, onSaved, canEdit = false }: Props) {
  const { title, collection, placeholder, list, save } = kinds[kind];
  const version = useEditVersion(`/services/${serviceId}/${collection}`);
  const [available, setAvailable] = useState<ComboboxOption[]>([]);
  const [newId, setNewId] = useState("");
  const [newTargetPath, setNewTargetPath] = useState("");

  const fetchAvailable = useCallback(() => {
    list()
      .then((items) => {
        setAvailable(
          items.map(({ ID, Spec }) => ({
            value: ID,
            label: Spec.Name,
            description: ID.slice(0, 12),
          })),
        );
      })
      .catch(console.warn);
  }, [list]);

  function handleSelected(id: string, draft: DataRef[]) {
    setNewId(id);

    const match = available.find(({ value }) => value === id);

    if (match) {
      setNewTargetPath(defaultTarget(kind, match.label, refs, draft));
    }
  }

  return (
    <EditableTable<DataRef>
      title={title}
      items={refs}
      columns={[kind === "config" ? "Config" : "Secret", "Target"]}
      defaultOpen={refs.length > 0}
      editDisabled={!canEdit}
      onEditStart={() => {
        version.capture();
        fetchAvailable();
      }}
      emptyLabel={`No ${collection} attached`}
      emptyHint={`Click Edit to attach Docker ${collection} to this service.`}
      keyFn={({ id }) => id}
      renderReadOnly={(items) => (
        <SimpleTable
          columns={["Name", "Target"]}
          items={items}
          keyFn={({ id }) => id}
          renderRow={({ id, name, fileName }) => (
            <>
              <td className="p-3 text-sm">
                <Link
                  to={`/${collection}/${id}`}
                  className="text-link hover:underline"
                >
                  <ResourceName name={name} />
                </Link>
              </td>
              <td className="p-3 font-mono text-sm">{fileName}</td>
            </>
          )}
        />
      )}
      renderKeyCell={({ name }) => (
        <span className="text-xs">
          <ResourceName name={name} />
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
          value={newId}
          onChange={(id) => handleSelected(id, draft)}
          options={available}
          placeholder={`Select ${kind}...`}
          allowCustom={false}
        />
      )}
      renderAddValueCell={() => (
        <Input
          value={newTargetPath}
          onChange={(event) => setNewTargetPath(event.target.value)}
          placeholder={placeholder}
        />
      )}
      canAdd={!!newId && !!newTargetPath}
      onAddCommit={() => {
        if (!newId || !newTargetPath) {
          return null;
        }

        const match = available.find(({ value }) => value === newId);

        return { id: newId, name: match?.label ?? newId, fileName: newTargetPath };
      }}
      onAddReset={() => {
        setNewId("");
        setNewTargetPath("");
      }}
      onSave={async (items) => {
        await save(serviceId, items, await version.ifMatch());
        onSaved();
      }}
    />
  );
}
