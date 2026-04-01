import { DockerDocsLink } from "./DockerDocsLink";
import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { LogDriver } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

const logDriverOptions = [
  { value: "json-file", label: "json-file", description: "JSON log files on the host (default)" },
  { value: "local", label: "local", description: "Compressed, rotated logs with minimal overhead" },
  { value: "journald", label: "journald", description: "systemd journal" },
  { value: "syslog", label: "syslog", description: "Syslog daemon" },
  {
    value: "gelf",
    label: "gelf",
    description: "Graylog Extended Log Format (e.g. Graylog, Logstash)",
  },
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

export function LogDriverEditor({
  serviceId,
  logDriver,
  onSaved,
  canEdit = false,
}: LogDriverEditorProps & { canEdit?: boolean }) {
  const [driverName, setDriverName] = useState("");
  const [options, setOptions] = useState<[string, string][]>([]);

  function resetForm() {
    setDriverName(logDriver?.Name ?? "");
    setOptions(logDriver?.Options ? Object.entries(logDriver.Options) : []);
  }

  function updateOption(index: number, part: 0 | 1, value: string) {
    const updated = [...options];
    updated[index] = [...updated[index]] as [string, string];
    updated[index][part] = value;
    setOptions(updated);
  }

  async function save() {
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

    onSaved();
  }

  return (
    <EditablePanel
      title="Log Driver"
      empty={!logDriver}
      emptyDescription="Click Edit to choose how container logs are collected and forwarded."
      canEdit={canEdit}
      onOpen={resetForm}
      onSave={save}
      actions={
        <Button
          variant="outline"
          size="sm"
          onClick={() => setOptions([...options, ["", ""]])}
        >
          <Plus className="size-3" />
          Add option
        </Button>
      }
      display={
        logDriver ? (
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
        ) : null
      }
      edit={
        <>
          <div className="flex flex-col gap-1.5">
            <label className="flex items-center gap-1 text-xs font-medium text-foreground">
              Driver name{" "}
              <DockerDocsLink href="https://docs.docker.com/reference/compose-file/services/#logging" />
            </label>

            <Combobox
              value={driverName}
              onChange={setDriverName}
              placeholder="Select driver..."
              options={logDriverOptions}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="flex items-center gap-1 text-xs font-medium text-foreground">
              Options{" "}
              <DockerDocsLink href="https://docs.docker.com/reference/compose-file/services/#logging" />
            </label>

            {options.map(([key, value], index) => (
              <div
                key={index}
                className="flex items-center gap-2"
              >
                <Input
                  value={key}
                  onChange={(event) => updateOption(index, 0, event.target.value)}
                  placeholder="key"
                  className="font-mono"
                />

                <Input
                  value={value}
                  onChange={(event) => updateOption(index, 1, event.target.value)}
                  placeholder="value"
                  className="font-mono"
                />

                <Button
                  variant="outline"
                  size="xs"
                  className="h-8 shrink-0"
                  onClick={() => setOptions(options.filter((_, i) => i !== index))}
                >
                  <Trash2 className="size-3" />
                </Button>
              </div>
            ))}
          </div>
        </>
      }
    />
  );
}
