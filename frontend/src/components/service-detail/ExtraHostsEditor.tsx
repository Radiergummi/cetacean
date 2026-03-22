import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, X } from "lucide-react";
import type { MouseEvent } from "react";
import { useState } from "react";

type HostRow = { ip: string; hostname: string };

function parseHosts(hosts: string[] | undefined): HostRow[] {
  if (!hosts || hosts.length === 0) {
    return [];
  }

  return hosts.map((entry) => {
    const spaceIndex = entry.indexOf(" ");
    if (spaceIndex === -1) {
      return { ip: entry, hostname: "" };
    }

    return {
      ip: entry.slice(0, spaceIndex),
      hostname: entry.slice(spaceIndex + 1),
    };
  });
}

export function ExtraHostsEditor({
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
  const [rows, setRows] = useState<HostRow[]>([]);

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setRows(parseHosts(config.hosts));
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addRow() {
    setRows((previous) => [...previous, { ip: "", hostname: "" }]);
  }

  function removeRow(index: number) {
    setRows((previous) => previous.filter((_, i) => i !== index));
  }

  function updateRow(index: number, field: keyof HostRow, value: string) {
    setRows((previous) =>
      previous.map((row, i) => (i === index ? { ...row, [field]: value } : row)),
    );
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const hostEntries = rows
        .filter(({ ip, hostname }) => ip.trim() && hostname.trim())
        .map(({ ip, hostname }) => `${ip.trim()} ${hostname.trim()}`);

      const patch: Record<string, unknown> = {
        hosts: hostEntries.length > 0 ? hostEntries : null,
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
        <div className="space-y-3">
          <div className="space-y-2">
            {rows.map((row, index) => (
              <div
                key={index}
                className="flex items-center gap-2"
              >
                <input
                  type="text"
                  value={row.ip}
                  onChange={(event) => updateRow(index, "ip", event.target.value)}
                  placeholder="192.168.1.1"
                  className="h-8 w-40 rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                />
                <input
                  type="text"
                  value={row.hostname}
                  onChange={(event) => updateRow(index, "hostname", event.target.value)}
                  placeholder="myhost"
                  className="h-8 flex-1 rounded-md border bg-background px-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                />
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => removeRow(index)}
                  disabled={saving}
                >
                  <X className="size-3" />
                </Button>
              </div>
            ))}
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={addRow}
            disabled={saving}
          >
            <Plus className="size-3" />
            Add Row
          </Button>

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
          <div className="flex-1">
            {config.hosts && config.hosts.length > 0 ? (
              <table className="w-full text-sm">
                <thead>
                  <tr>
                    <th className="pr-4 pb-1 text-left font-normal text-muted-foreground">
                      IP Address
                    </th>
                    <th className="pb-1 text-left font-normal text-muted-foreground">Hostname</th>
                  </tr>
                </thead>
                <tbody>
                  {parseHosts(config.hosts).map((row, index) => (
                    <tr key={index}>
                      <td className="pr-4 font-mono">{row.ip}</td>
                      <td className="font-mono">{row.hostname}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <span className="text-sm text-muted-foreground">None</span>
            )}
          </div>

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
