import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import type { MouseEvent } from "react";
import { useState } from "react";

export function CommandEditor({
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
  const [commandInput, setCommandInput] = useState("");
  const [argsInput, setArgsInput] = useState("");
  const [dirInput, setDirInput] = useState("");
  const [userInput, setUserInput] = useState("");

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setCommandInput(config.command?.join(" ") ?? "");
    setArgsInput(config.args?.join(" ") ?? "");
    setDirInput(config.dir);
    setUserInput(config.user);
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
      const patch: Record<string, unknown> = {};
      const cmd = commandInput.trim() ? commandInput.trim().split(/\s+/) : null;
      const argsList = argsInput.trim() ? argsInput.trim().split(/\s+/) : null;
      patch.command = cmd;
      patch.args = argsList;
      patch.dir = dirInput;
      patch.user = userInput;

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
            <label className="text-xs text-muted-foreground">Command</label>
            <input
              type="text"
              value={commandInput}
              onChange={(event) => setCommandInput(event.target.value)}
              placeholder="/bin/my-entrypoint"
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
            <p className="text-xs text-muted-foreground">Space-separated list of tokens</p>
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Args</label>
            <input
              type="text"
              value={argsInput}
              onChange={(event) => setArgsInput(event.target.value)}
              placeholder="--flag value"
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
            <p className="text-xs text-muted-foreground">Space-separated list of tokens</p>
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Working Dir</label>
            <input
              type="text"
              value={dirInput}
              onChange={(event) => setDirInput(event.target.value)}
              placeholder="/app"
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">User</label>
            <input
              type="text"
              value={userInput}
              onChange={(event) => setUserInput(event.target.value)}
              placeholder="nobody"
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
            <Row
              label="Command"
              value={config.command?.join(" ")}
              mono
            />
            <Row
              label="Args"
              value={config.args?.join(" ")}
              mono
            />
            <Row
              label="Working Dir"
              value={config.dir}
            />
            <Row
              label="User"
              value={config.user}
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

function Row({ label, value, mono }: { label: string; value: string | undefined; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={mono ? "font-mono" : undefined}>{value || "—"}</dd>
    </div>
  );
}
