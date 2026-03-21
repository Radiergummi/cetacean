import { api } from "@/api/client";
import type { LogDriver } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

const logDriverOptions = [
  { value: "json-file", label: "json-file", description: "JSON log files on the host (default)" },
  { value: "local", label: "local", description: "Compressed, rotated logs with minimal overhead" },
  { value: "journald", label: "journald", description: "systemd journal" },
  { value: "syslog", label: "syslog", description: "Syslog daemon" },
  { value: "gelf", label: "gelf", description: "Graylog Extended Log Format (e.g. Graylog, Logstash)" },
  { value: "fluentd", label: "fluentd", description: "Fluentd forward protocol" },
  { value: "awslogs", label: "awslogs", description: "Amazon CloudWatch Logs" },
  { value: "gcplogs", label: "gcplogs", description: "Google Cloud Logging" },
  { value: "splunk", label: "splunk", description: "Splunk HTTP Event Collector" },
  { value: "loki", label: "loki", description: "Grafana Loki" },
  { value: "none", label: "none", description: "Discard all logs" },
];

interface LogDriverEditorProps {
  serviceId: string;
  logDriver: LogDriver | null;
  onSaved: () => void;
}

export function LogDriverEditor({ serviceId, logDriver, onSaved }: LogDriverEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  useEscapeCancel(editing, () => cancelEdit());

  const [driverName, setDriverName] = useState("");
  const [options, setOptions] = useState<[string, string][]>([]);

  function openEdit() {
    setDriverName(logDriver?.Name ?? "");
    setOptions(logDriver?.Options ? Object.entries(logDriver.Options) : []);
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

  if (!editing && !logDriver && !canEdit) {
    return null;
  }

  if (!editing) {
    return (
      <div className="rounded-lg border p-3 space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Log Driver
          </h3>

          {canEdit && (
            <Button
              variant="outline"
              size="xs"
              onClick={openEdit}
            >
              <Pencil className="size-3" />
              Edit
            </Button>
          )}
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
          <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
            <p className="text-sm">No log driver configured</p>
            {canEdit && <p className="text-xs">Click Edit to choose how container logs are collected and forwarded.</p>}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="rounded-lg border p-3 space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Log Driver
      </h3>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-muted-foreground">Driver name</label>

        <Combobox
          value={driverName}
          onChange={setDriverName}
          placeholder="Select driver..."
          options={logDriverOptions}
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-muted-foreground">Options</label>

        {options.map(([key, value], index) => (
          <div
            key={index}
            className="flex items-center gap-2"
          >
            <input
              value={key}
              onChange={(event) => updateOption(index, 0, event.target.value)}
              placeholder="key"
              className="h-8 w-full rounded-md border border-input bg-transparent px-3 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />

            <input
              value={value}
              onChange={(event) => updateOption(index, 1, event.target.value)}
              placeholder="value"
              className="h-8 w-full rounded-md border border-input bg-transparent px-3 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />

            <Button
              variant="outline"
              size="xs"
              onClick={() => removeOption(index)}
            >
              <Trash2 className="size-3" />
            </Button>
          </div>
        ))}
      </div>

      {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

      <footer className="flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={addOption}
        >
          <Plus className="size-3" />
          Add option
        </Button>

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
  );
}
