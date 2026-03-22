import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { DescriptionRow } from "@/components/data";
import { Input } from "@/components/ui/input";
import { useState } from "react";

function splitComma(value: string): string[] | undefined {
  const items = value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  return items.length > 0 ? items : undefined;
}

export function DnsEditor({
  serviceId,
  config,
  onSaved,
}: {
  serviceId: string;
  config: ContainerConfig;
  onSaved: (updated: ContainerConfig) => void;
}) {
  const [nameserversInput, setNameserversInput] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const [optionsInput, setOptionsInput] = useState("");

  function resetForm() {
    setNameserversInput(config.dnsConfig?.nameservers?.join(", ") ?? "");
    setSearchInput(config.dnsConfig?.search?.join(", ") ?? "");
    setOptionsInput(config.dnsConfig?.options?.join(", ") ?? "");
  }

  async function save() {
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
  }

  return (
    <EditablePanel
      onOpen={resetForm}
      onSave={save}
      display={
        config.dnsConfig == null ? (
          <p className="text-sm text-muted-foreground">Default</p>
        ) : (
          <dl className="grid gap-y-2 text-sm">
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
        )
      }
      edit={
        <>
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
        </>
      }
    />
  );
}
