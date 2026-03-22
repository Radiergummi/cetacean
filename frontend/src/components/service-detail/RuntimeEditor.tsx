import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { DescriptionRow } from "@/components/data";
import { Input } from "@/components/ui/input";
import { formatDuration } from "@/lib/format";
import { renderSwarmTemplate } from "@/lib/swarmTemplates";
import { useState } from "react";

function formatInit(init: boolean | undefined): string {
  if (init === undefined) {
    return "Default";
  }

  return init ? "Yes" : "No";
}

export function RuntimeEditor({
  serviceId,
  config,
  onSaved,
}: {
  serviceId: string;
  config: ContainerConfig;
  onSaved: (updated: ContainerConfig) => void;
}) {
  const [hostnameInput, setHostnameInput] = useState("");
  const [initValue, setInitValue] = useState<boolean | undefined>(undefined);
  const [ttyInput, setTtyInput] = useState(false);
  const [readOnlyInput, setReadOnlyInput] = useState(false);
  const [stopSignalInput, setStopSignalInput] = useState("");
  const [gracePeriodInput, setGracePeriodInput] = useState("");

  function resetForm() {
    setHostnameInput(config.hostname);
    setInitValue(config.init);
    setTtyInput(config.tty);
    setReadOnlyInput(config.readOnly);
    setStopSignalInput(config.stopSignal);
    setGracePeriodInput(config.stopGracePeriod != null ? String(config.stopGracePeriod / 1e9) : "");
  }

  async function save() {
    const patch: Record<string, unknown> = {
      hostname: hostnameInput,
      tty: ttyInput,
      readOnly: readOnlyInput,
      stopSignal: stopSignalInput,
    };

    if (initValue === undefined) {
      patch.init = null;
    } else {
      patch.init = initValue;
    }

    if (gracePeriodInput !== "") {
      patch.stopGracePeriod = parseFloat(gracePeriodInput) * 1e9;
    } else {
      patch.stopGracePeriod = null;
    }

    const updated = await api.patchServiceContainerConfig(serviceId, patch);
    onSaved(updated);
  }

  return (
    <EditablePanel
      title="Runtime"
      onOpen={resetForm}
      onSave={save}
      display={
        <dl className="grid gap-y-2 text-sm">
          <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
            <dt className="text-muted-foreground">Hostname</dt>
            <dd className="font-mono">
              {config.hostname ? renderSwarmTemplate(config.hostname) : "—"}
            </dd>
          </div>
          <DescriptionRow
            label="Init"
            value={formatInit(config.init)}
          />
          <DescriptionRow
            label="TTY"
            value={config.tty ? "Yes" : "No"}
          />
          <DescriptionRow
            label="Read Only"
            value={config.readOnly ? "Yes" : "No"}
          />
          <DescriptionRow
            label="Stop Signal"
            value={config.stopSignal || undefined}
          />
          <DescriptionRow
            label="Stop Grace Period"
            value={
              config.stopGracePeriod != null ? formatDuration(config.stopGracePeriod) : undefined
            }
          />
        </dl>
      }
      edit={
        <>
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Hostname</label>
            <Input
              value={hostnameInput}
              onChange={(event) => setHostnameInput(event.target.value)}
              placeholder="{{.Node.Hostname}}-{{.Task.Slot}}"
              className="font-mono"
            />
            <p className="text-xs text-muted-foreground">
              Supports swarm templates: {"{{.Node.Hostname}}"}, {"{{.Task.Slot}}"}, etc.
            </p>
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Stop Signal</label>
            <Input
              value={stopSignalInput}
              onChange={(event) => setStopSignalInput(event.target.value)}
              placeholder="SIGTERM"
              className="font-mono"
            />
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Stop Grace Period (seconds)</label>
            <Input
              type="number"
              value={gracePeriodInput}
              onChange={(event) => setGracePeriodInput(event.target.value)}
              placeholder="10"
              min={0}
              step={0.1}
              className="font-mono"
            />
          </div>

          <div className="flex flex-col gap-2">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={initValue === true}
                onChange={(event) => setInitValue(event.target.checked)}
                className="size-4"
              />
              Init
              {initValue === undefined ? (
                <span className="text-xs text-muted-foreground">(default)</span>
              ) : (
                <button
                  type="button"
                  onClick={() => setInitValue(undefined)}
                  className="text-xs text-muted-foreground underline hover:text-foreground"
                >
                  Reset to default
                </button>
              )}
            </label>

            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={ttyInput}
                onChange={(event) => setTtyInput(event.target.checked)}
                className="size-4"
              />
              TTY
            </label>

            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={readOnlyInput}
                onChange={(event) => setReadOnlyInput(event.target.checked)}
                className="size-4"
              />
              Read Only
            </label>
          </div>
        </>
      }
    />
  );
}
