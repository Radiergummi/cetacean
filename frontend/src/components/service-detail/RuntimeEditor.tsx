import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { DescriptionRow } from "@/components/data";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import type { MouseEvent } from "react";
import { useState } from "react";

function formatGracePeriod(nanoseconds: number | undefined): string {
  if (nanoseconds == null) {
    return "—";
  }

  const seconds = nanoseconds / 1e9;

  return `${seconds}s`;
}

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
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const [hostnameInput, setHostnameInput] = useState("");
  const [initValue, setInitValue] = useState<boolean | undefined>(undefined);
  const [ttyInput, setTtyInput] = useState(false);
  const [readOnlyInput, setReadOnlyInput] = useState(false);
  const [stopSignalInput, setStopSignalInput] = useState("");
  const [gracePeriodInput, setGracePeriodInput] = useState("");

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setHostnameInput(config.hostname);
    setInitValue(config.init);
    setTtyInput(config.tty);
    setReadOnlyInput(config.readOnly);
    setStopSignalInput(config.stopSignal);
    setGracePeriodInput(config.stopGracePeriod != null ? String(config.stopGracePeriod / 1e9) : "");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
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
          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Hostname</label>
            <input
              type="text"
              value={hostnameInput}
              onChange={(event) => setHostnameInput(event.target.value)}
              placeholder="my-container"
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Stop Signal</label>
            <input
              type="text"
              value={stopSignalInput}
              onChange={(event) => setStopSignalInput(event.target.value)}
              placeholder="SIGTERM"
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Stop Grace Period (seconds)</label>
            <input
              type="number"
              value={gracePeriodInput}
              onChange={(event) => setGracePeriodInput(event.target.value)}
              placeholder="10"
              min={0}
              step={0.1}
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
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
            <DescriptionRow
              label="Hostname"
              value={config.hostname || undefined}
            />
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
              value={formatGracePeriod(config.stopGracePeriod)}
            />
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
