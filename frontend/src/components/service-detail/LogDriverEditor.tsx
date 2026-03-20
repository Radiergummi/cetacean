import { api } from "@/api/client";
import type { LogDriver } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

interface LogDriverEditorProps {
  serviceId: string;
  logDriver: LogDriver | null;
  onSaved: () => void;
}

export function LogDriverEditor({ serviceId, logDriver, onSaved }: LogDriverEditorProps) {
  const { level } = useOperationsLevel();
  const canEdit = level >= 1;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const [driverName, setDriverName] = useState("");
  const [options, setOptions] = useState<[string, string][]>([]);

  function openEdit() {
    setDriverName(logDriver?.Name ?? "");
    setOptions(
      logDriver?.Options
        ? Object.entries(logDriver.Options)
        : [],
    );
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addOption() {
    setOptions([...options, ["", ""]]);
  }

  function removeOption(index: number) {
    setOptions(options.filter((_, i) => i !== index));
  }

  function updateOption(index: number, part: 0 | 1, value: string) {
    const updated = [...options];
    updated[index] = [...updated[index]] as [string, string];
    updated[index][part] = value;
    setOptions(updated);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const optionsMap: Record<string, string> = {};

      for (const [key, value] of options) {
        if (key.trim()) {
          optionsMap[key.trim()] = value;
        }
      }

      await api.patchServiceLogDriver(serviceId, {
        Name: driverName,
        Options: Object.keys(optionsMap).length > 0 ? optionsMap : undefined,
      });

      setEditing(false);
      onSaved();
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update log driver"));
    } finally {
      setSaving(false);
    }
  }

  if (!editing) {
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Log Driver
          </h3>

          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
            disabled={!canEdit}
            title={canEdit ? undefined : "Editing disabled by server configuration"}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        </div>

        {logDriver ? (
          <div className="space-y-2">
            <div className="flex justify-between text-sm">
              <span className="text-muted-foreground">Driver</span>
              <span className="font-mono">{logDriver.Name}</span>
            </div>

            {logDriver.Options && Object.keys(logDriver.Options).length > 0 && (
              <div className="flex flex-wrap gap-2">
                {Object.entries(logDriver.Options).map(([key, value]) => (
                  <span
                    key={key}
                    className="inline-flex items-center rounded-md border px-2 py-0.5 font-mono text-xs"
                  >
                    <span className="text-muted-foreground">{key}=</span>
                    {value}
                  </span>
                ))}
              </div>
            )}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No log driver configured.</p>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Log Driver
      </h3>

      <div className="space-y-1">
        <label className="text-xs font-medium text-muted-foreground">Driver name</label>

        <Input
          value={driverName}
          onChange={(event) => setDriverName(event.target.value)}
          placeholder="json-file"
          className="w-64"
        />
      </div>

      <div className="space-y-2">
        <label className="text-xs font-medium text-muted-foreground">Options</label>

        {options.map(([key, value], index) => (
          <div key={index} className="flex items-center gap-2">
            <Input
              value={key}
              onChange={(event) => updateOption(index, 0, event.target.value)}
              placeholder="key"
              className="font-mono text-sm"
            />

            <Input
              value={value}
              onChange={(event) => updateOption(index, 1, event.target.value)}
              placeholder="value"
              className="font-mono text-sm"
            />

            <Button variant="outline" size="xs" onClick={() => removeOption(index)}>
              <Trash2 className="size-3" />
            </Button>
          </div>
        ))}

        <Button variant="outline" size="sm" onClick={addOption}>
          <Plus className="size-3" />
          Add option
        </Button>
      </div>

      {saveError && <p className="text-xs text-red-600">{saveError}</p>}

      <div className="flex gap-2">
        <Button size="sm" onClick={save} disabled={saving}>
          {saving && <Spinner className="size-3" />}
          Save
        </Button>

        <Button variant="outline" size="sm" onClick={cancelEdit} disabled={saving}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
