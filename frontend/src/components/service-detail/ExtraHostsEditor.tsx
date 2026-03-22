import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Plus, X } from "lucide-react";
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
  const [rows, setRows] = useState<HostRow[]>([]);

  function resetForm() {
    setRows(parseHosts(config.hosts));
  }

  function updateRow(index: number, field: keyof HostRow, value: string) {
    setRows((previous) =>
      previous.map((row, i) => (i === index ? { ...row, [field]: value } : row)),
    );
  }

  async function save() {
    const hostEntries = rows
      .filter(({ ip, hostname }) => ip.trim() && hostname.trim())
      .map(({ ip, hostname }) => `${ip.trim()} ${hostname.trim()}`);

    const updated = await api.patchServiceContainerConfig(serviceId, {
      hosts: hostEntries.length > 0 ? hostEntries : null,
    });
    onSaved(updated);
  }

  return (
    <EditablePanel
      onOpen={resetForm}
      onSave={save}
      display={
        config.hosts && config.hosts.length > 0 ? (
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
        )
      }
      edit={
        <>
          <div className="space-y-2">
            {rows.map((row, index) => (
              <div
                key={index}
                className="flex items-center gap-2"
              >
                <Input
                  value={row.ip}
                  onChange={(event) => updateRow(index, "ip", event.target.value)}
                  placeholder="192.168.1.1"
                  className="w-40 font-mono"
                />
                <Input
                  value={row.hostname}
                  onChange={(event) => updateRow(index, "hostname", event.target.value)}
                  placeholder="myhost"
                  className="flex-1 font-mono"
                />
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => setRows((previous) => previous.filter((_, i) => i !== index))}
                >
                  <X className="size-3" />
                </Button>
              </div>
            ))}
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={() => setRows((previous) => [...previous, { ip: "", hostname: "" }])}
          >
            <Plus className="size-3" />
            Add Row
          </Button>
        </>
      }
    />
  );
}
