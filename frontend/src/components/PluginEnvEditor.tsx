import { api } from "@/api/client";
import type { PluginEnv } from "@/api/types";
import { EditablePanel } from "@/components/service-detail/EditablePanel";
import { useMemo, useState } from "react";

function parseEnvArray(entries: string[]): Record<string, string> {
  const record: Record<string, string> = {};

  for (const entry of entries) {
    const index = entry.indexOf("=");

    if (index > 0) {
      record[entry.slice(0, index)] = entry.slice(index + 1);
    }
  }

  return record;
}

interface PluginEnvEditorProps {
  pluginName: string;
  /** Declared variables from Config.Env (schema + defaults) */
  declarations: PluginEnv[];
  /** Raw Settings.Env entries (e.g. ["KEY=value", ...]) */
  values: string[];
  onSaved: () => void;
}

export function PluginEnvEditor({
  pluginName,
  declarations,
  values,
  onSaved,
}: PluginEnvEditorProps) {
  const currentValues = useMemo(() => parseEnvArray(values), [values]);
  const [draft, setDraft] = useState<Record<string, string>>({});

  function updateDraft(name: string, value: string) {
    setDraft((previous) => ({ ...previous, [name]: value }));
  }

  return (
    <EditablePanel
      title="Environment Variables"
      empty={declarations.length === 0}
      emptyDescription="This plugin does not declare any environment variables."
      onOpen={() => {
        const initial: Record<string, string> = {};

        for (const { Name: envName, Value: defaultValue } of declarations) {
          initial[envName] = currentValues[envName] ?? defaultValue ?? "";
        }

        setDraft(initial);
      }}
      onSave={async () => {
        const envArray = Object.entries(draft).map(([key, value]) => `${key}=${value}`);
        await api.configurePlugin(pluginName, { env: envArray });
        onSaved();
      }}
      display={
        <div className="space-y-3">
          {declarations.map(({ Name: envName, Description, Value: defaultValue }) => {
            const current = currentValues[envName];
            const isDefault = current == null || current === (defaultValue ?? "");

            return (
              <div
                key={envName}
                className="space-y-0.5"
              >
                <div className="flex items-baseline gap-2">
                  <span className="font-mono text-xs font-medium">{envName}</span>
                  {isDefault && (
                    <span className="text-[10px] text-muted-foreground">default</span>
                  )}
                </div>
                <div className="font-mono text-xs text-muted-foreground">
                  {current ?? defaultValue ?? "—"}
                </div>
                {Description && (
                  <p className="text-xs text-muted-foreground/70">{Description}</p>
                )}
              </div>
            );
          })}
        </div>
      }
      edit={
        <div className="space-y-4">
          {declarations.map(({ Name: envName, Description, Value: defaultValue }) => (
            <label
              key={envName}
              className="block space-y-1"
            >
              <span className="flex items-baseline justify-between">
                <span className="font-mono text-xs font-medium">{envName}</span>
                {defaultValue && (
                  <span className="text-[11px] text-muted-foreground/70">
                    Default: <code className="rounded bg-muted px-1">{defaultValue}</code>
                  </span>
                )}
              </span>
              <input
                type="text"
                value={draft[envName] ?? ""}
                onChange={(event) => updateDraft(envName, event.target.value)}
                placeholder={defaultValue ?? ""}
                className="h-8 w-full rounded-md border bg-transparent px-2 font-mono text-xs outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
              />
              {Description && (
                <p className="text-xs text-muted-foreground">{Description}</p>
              )}
            </label>
          ))}
        </div>
      }
    />
  );
}
