import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, X } from "lucide-react";
import type { KeyboardEvent, MouseEvent } from "react";
import { useState } from "react";

function CapabilityBadges({
  items,
  onRemove,
}: {
  items: string[];
  onRemove?: (cap: string) => void;
}) {
  if (items.length === 0) {
    return <span className="text-muted-foreground">None</span>;
  }

  return (
    <div className="flex flex-wrap gap-1">
      {items.map((cap) => (
        <span
          key={cap}
          className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
        >
          {cap}
          {onRemove && (
            <button
              type="button"
              onClick={() => onRemove(cap)}
              className="text-muted-foreground hover:text-foreground"
              aria-label={`Remove ${cap}`}
            >
              <X className="size-3" />
            </button>
          )}
        </span>
      ))}
    </div>
  );
}

export function CapabilitiesEditor({
  serviceId,
  config,
  onSaved,
}: {
  serviceId: string;
  config: ContainerConfig;
  onSaved: (updated: ContainerConfig) => void;
}) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [addList, setAddList] = useState<string[]>([]);
  const [dropList, setDropList] = useState<string[]>([]);
  const [addInput, setAddInput] = useState("");
  const [dropInput, setDropInput] = useState("");

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setAddList(config.capabilityAdd ?? []);
    setDropList(config.capabilityDrop ?? []);
    setAddInput("");
    setDropInput("");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function handleKeyDown(
    event: KeyboardEvent<HTMLInputElement>,
    input: string,
    list: string[],
    setList: (list: string[]) => void,
    setInput: (value: string) => void,
  ) {
    if (event.key !== "Enter") {
      return;
    }

    event.preventDefault();

    const value = input.trim().toUpperCase();

    if (value && !list.includes(value)) {
      setList([...list, value]);
    }

    setInput("");
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const patch: Record<string, unknown> = {
        capabilityAdd: addList.length > 0 ? addList : null,
        capabilityDrop: dropList.length > 0 ? dropList : null,
      };

      const updated = await api.patchServiceContainerConfig(serviceId, patch);
      onSaved(updated);
      setEditing(false);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Save failed"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-lg border p-3">
      {editing ? (
        <div className="space-y-4">
          <div className="flex flex-col gap-2">
            <label className="text-xs text-muted-foreground">Add Capabilities</label>
            <CapabilityBadges
              items={addList}
              onRemove={(cap) => setAddList(addList.filter((c) => c !== cap))}
            />
            <Input
              value={addInput}
              onChange={(event) => setAddInput(event.target.value.toUpperCase())}
              onKeyDown={(event) =>
                handleKeyDown(event, addInput, addList, setAddList, setAddInput)
              }
              placeholder="NET_ADMIN — press Enter to add"
              className="font-mono"
            />
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-xs text-muted-foreground">Drop Capabilities</label>
            <CapabilityBadges
              items={dropList}
              onRemove={(cap) => setDropList(dropList.filter((c) => c !== cap))}
            />
            <Input
              value={dropInput}
              onChange={(event) => setDropInput(event.target.value.toUpperCase())}
              onKeyDown={(event) =>
                handleKeyDown(event, dropInput, dropList, setDropList, setDropInput)
              }
              placeholder="ALL — press Enter to add"
              className="font-mono"
            />
          </div>

          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

          <footer className="flex items-center justify-end gap-2">
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
          </footer>
        </div>
      ) : (
        <div className="flex items-start justify-between gap-2">
          <dl className="grid flex-1 gap-y-2 text-sm">
            <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
              <dt className="text-muted-foreground">Add</dt>
              <dd>
                <CapabilityBadges items={config.capabilityAdd ?? []} />
              </dd>
            </div>

            <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
              <dt className="text-muted-foreground">Drop</dt>
              <dd>
                <CapabilityBadges items={config.capabilityDrop ?? []} />
              </dd>
            </div>
          </dl>

          {canEdit && (
            <Button
              variant="outline"
              size="xs"
              onClick={(event: MouseEvent) => {
                event.stopPropagation();
                openEdit();
              }}
            >
              <Pencil className="size-3" />
              Edit
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
