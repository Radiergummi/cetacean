import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { DescriptionRow } from "@/components/data";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import type { MouseEvent } from "react";
import { useState } from "react";

export function DnsEditor({
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
  const [nameserversInput, setNameserversInput] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const [optionsInput, setOptionsInput] = useState("");

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setNameserversInput(config.dnsConfig?.nameservers?.join(", ") ?? "");
    setSearchInput(config.dnsConfig?.search?.join(", ") ?? "");
    setOptionsInput(config.dnsConfig?.options?.join(", ") ?? "");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function splitComma(value: string): string[] | undefined {
    const items = value
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    return items.length > 0 ? items : undefined;
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const nameservers = splitComma(nameserversInput);
      const search = splitComma(searchInput);
      const options = splitComma(optionsInput);

      const patch: Record<string, unknown> = {};
      if (!nameservers && !search && !options) {
        patch.dnsConfig = null;
      } else {
        patch.dnsConfig = { nameservers, search, options };
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
            <label className="text-xs text-muted-foreground">Nameservers</label>
            <Input
              value={nameserversInput}
              onChange={(event) => setNameserversInput(event.target.value)}
              placeholder="8.8.8.8, 1.1.1.1"
              className="font-mono"
            />
            <p className="text-xs text-muted-foreground">Comma-separated IP addresses</p>
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Search Domains</label>
            <Input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder="example.com, local"
              className="font-mono"
            />
            <p className="text-xs text-muted-foreground">Comma-separated domain names</p>
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Options</label>
            <Input
              value={optionsInput}
              onChange={(event) => setOptionsInput(event.target.value)}
              placeholder="ndots:5, timeout:2"
              className="font-mono"
            />
            <p className="text-xs text-muted-foreground">Comma-separated resolver options</p>
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
          {config.dnsConfig == null ? (
            <p className="flex-1 text-sm text-muted-foreground">Default</p>
          ) : (
            <dl className="grid flex-1 gap-y-2 text-sm">
              <DescriptionRow
                label="Nameservers"
                value={config.dnsConfig.nameservers?.join(", ")}
                mono
              />
              <DescriptionRow
                label="Search Domains"
                value={config.dnsConfig.search?.join(", ")}
                mono
              />
              <DescriptionRow
                label="Options"
                value={config.dnsConfig.options?.join(", ")}
                mono
              />
            </dl>
          )}

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
