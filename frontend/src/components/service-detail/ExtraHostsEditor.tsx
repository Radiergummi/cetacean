import { DockerDocsLink } from "./DockerDocsLink";
import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

type HostRow = { ip: string; hostname: string };

const ipPattern = /^(\d{1,3}\.){3}\d{1,3}$/;
const hostnamePattern = /^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$/;

function isValidIp(value: string): boolean {
  if (!ipPattern.test(value)) {
    return false;
  }

  return value.split(".").every((octet) => {
    const number = Number(octet);
    return number >= 0 && number <= 255;
  });
}

function isValidHostname(value: string): boolean {
  return value.length > 0 && value.length <= 253 && hostnamePattern.test(value);
}

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
    const parsed = parseHosts(config.hosts);
    setRows(parsed.length > 0 ? parsed : [{ ip: "", hostname: "" }]);
  }

  function updateRow(index: number, field: keyof HostRow, value: string) {
    setRows((previous) =>
      previous.map((row, i) => (i === index ? { ...row, [field]: value } : row)),
    );
  }

  function addRow() {
    setRows((previous) => [...previous, { ip: "", hostname: "" }]);
  }

  function removeRow(index: number) {
    setRows((previous) => {
      const next = previous.filter((_, i) => i !== index);
      return next.length > 0 ? next : [{ ip: "", hostname: "" }];
    });
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
      title="Extra Hosts"
      empty={!config.hosts || config.hosts.length === 0}
      emptyDescription="Click Edit to add custom /etc/hosts entries."
      onOpen={resetForm}
      onSave={save}
      actions={
        <Button
          variant="outline"
          size="sm"
          onClick={addRow}
        >
          <Plus className="size-3" />
          Add host
        </Button>
      }
      display={
        <table className="w-full text-sm">
          <thead>
            <tr>
              <th className="pr-4 pb-1 text-left font-normal text-muted-foreground">IP Address</th>
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
      }
      edit={
        <div className="space-y-2">
          <span className="flex items-center gap-1 text-xs text-foreground">
            Custom <code>/etc/hosts</code> entries{" "}
            <DockerDocsLink href="https://docs.docker.com/reference/compose-file/services/#extra_hosts" />
          </span>
          {rows.map((row, index) => {
            const ipTouched = row.ip.length > 0;
            const hostnameTouched = row.hostname.length > 0;
            const ipInvalid = ipTouched && !isValidIp(row.ip);
            const hostnameInvalid = hostnameTouched && !isValidHostname(row.hostname);

            return (
              <div
                key={index}
                className="flex items-start gap-2"
              >
                <div className="flex w-40 flex-col gap-0.5">
                  <Input
                    value={row.ip}
                    onChange={(event) => updateRow(index, "ip", event.target.value)}
                    placeholder="192.168.1.1"
                    className={cn("font-mono", ipInvalid && "border-red-500")}
                  />
                  {ipInvalid && <p className="text-[10px] text-red-500">Invalid IP address</p>}
                </div>
                <div className="flex flex-1 flex-col gap-0.5">
                  <Input
                    value={row.hostname}
                    onChange={(event) => updateRow(index, "hostname", event.target.value)}
                    placeholder="myhost"
                    className={cn("font-mono", hostnameInvalid && "border-red-500")}
                  />
                  {hostnameInvalid && <p className="text-[10px] text-red-500">Invalid hostname</p>}
                </div>
                <Button
                  variant="outline"
                  size="xs"
                  className="h-8 shrink-0"
                  onClick={() => removeRow(index)}
                >
                  <Trash2 className="size-3" />
                </Button>
              </div>
            );
          })}
        </div>
      }
    />
  );
}
