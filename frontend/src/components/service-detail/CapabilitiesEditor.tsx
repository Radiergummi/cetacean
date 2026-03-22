import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, X } from "lucide-react";
import type { KeyboardEvent, MouseEvent } from "react";
import { useState } from "react";

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

  function handleAddKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key !== "Enter") {
      return;
    }

    const value = addInput.trim().toUpperCase();

    if (value && !addList.includes(value)) {
      setAddList([...addList, value]);
    }

    setAddInput("");
  }

  function handleDropKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key !== "Enter") {
      return;
    }

    const value = dropInput.trim().toUpperCase();

    if (value && !dropList.includes(value)) {
      setDropList([...dropList, value]);
    }

    setDropInput("");
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
            <div className="flex flex-wrap gap-1">
              {addList.map((cap) => (
                <span
                  key={cap}
                  className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
                >
                  {cap}
                  <button
                    type="button"
                    onClick={() => setAddList(addList.filter((c) => c !== cap))}
                    className="text-muted-foreground hover:text-foreground"
                    aria-label={`Remove ${cap}`}
                  >
                    <X className="size-3" />
                  </button>
                </span>
              ))}
            </div>
            <input
              type="text"
              value={addInput}
              onChange={(event) => setAddInput(event.target.value.toUpperCase())}
              onKeyDown={handleAddKeyDown}
              placeholder="NET_ADMIN — press Enter to add"
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-xs text-muted-foreground">Drop Capabilities</label>
            <div className="flex flex-wrap gap-1">
              {dropList.map((cap) => (
                <span
                  key={cap}
                  className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
                >
                  {cap}
                  <button
                    type="button"
                    onClick={() => setDropList(dropList.filter((c) => c !== cap))}
                    className="text-muted-foreground hover:text-foreground"
                    aria-label={`Remove ${cap}`}
                  >
                    <X className="size-3" />
                  </button>
                </span>
              ))}
            </div>
            <input
              type="text"
              value={dropInput}
              onChange={(event) => setDropInput(event.target.value.toUpperCase())}
              onKeyDown={handleDropKeyDown}
              placeholder="ALL — press Enter to add"
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
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
                {config.capabilityAdd && config.capabilityAdd.length > 0 ? (
                  <div className="flex flex-wrap gap-1">
                    {config.capabilityAdd.map((cap) => (
                      <span
                        key={cap}
                        className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
                      >
                        {cap}
                      </span>
                    ))}
                  </div>
                ) : (
                  <span className="text-muted-foreground">None</span>
                )}
              </dd>
            </div>

            <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
              <dt className="text-muted-foreground">Drop</dt>
              <dd>
                {config.capabilityDrop && config.capabilityDrop.length > 0 ? (
                  <div className="flex flex-wrap gap-1">
                    {config.capabilityDrop.map((cap) => (
                      <span
                        key={cap}
                        className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
                      >
                        {cap}
                      </span>
                    ))}
                  </div>
                ) : (
                  <span className="text-muted-foreground">None</span>
                )}
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
