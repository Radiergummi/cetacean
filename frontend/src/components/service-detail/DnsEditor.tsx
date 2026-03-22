import { DockerDocsLink } from "./DockerDocsLink";
import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { DescriptionRow } from "@/components/data";
import { MultiCombobox } from "@/components/ui/multi-combobox";
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
  const [nameservers, setNameservers] = useState<string[]>([]);
  const [searchDomains, setSearchDomains] = useState<string[]>([]);
  const [resolverOptions, setResolverOptions] = useState<string[]>([]);

  function resetForm() {
    setNameservers(config.dnsConfig?.nameservers ?? []);
    setSearchDomains(config.dnsConfig?.search ?? []);
    setResolverOptions(config.dnsConfig?.options ?? []);
  }

  async function save() {
    const patch: Record<string, unknown> = {};

    if (!nameservers.length && !searchDomains.length && !resolverOptions.length) {
      patch.dnsConfig = null;
    } else {
      patch.dnsConfig = {
        nameservers: nameservers.length > 0 ? nameservers : undefined,
        search: searchDomains.length > 0 ? searchDomains : undefined,
        options: resolverOptions.length > 0 ? resolverOptions : undefined,
      };
    }

    const updated = await api.patchServiceContainerConfig(serviceId, patch);
    onSaved(updated);
  }

  return (
    <EditablePanel
      title="DNS"
      empty={config.dnsConfig == null}
      emptyDescription="Click Edit to configure custom DNS settings."
      onOpen={resetForm}
      onSave={save}
      display={
        <dl className="grid gap-y-2 text-sm">
          <DescriptionRow
            label="Nameservers"
            value={config.dnsConfig?.nameservers?.join(", ")}
            mono
          />
          <DescriptionRow
            label="Search Domains"
            value={config.dnsConfig?.search?.join(", ")}
            mono
          />
          <DescriptionRow
            label="Options"
            value={config.dnsConfig?.options?.join(", ")}
            mono
          />
        </dl>
      }
      edit={
        <>
          <div className="flex flex-col gap-1.5">
            <label className="flex items-center gap-1 text-xs text-muted-foreground">
              Nameservers <DockerDocsLink anchor="dns" />
            </label>
            <MultiCombobox
              values={nameservers}
              onChange={setNameservers}
              options={[]}
              placeholder="Type an IP address and press Enter..."
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="flex items-center gap-1 text-xs text-muted-foreground">
              Search Domains <DockerDocsLink anchor="dns-search" />
            </label>
            <MultiCombobox
              values={searchDomains}
              onChange={setSearchDomains}
              options={[]}
              placeholder="Type a domain and press Enter..."
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="flex items-center gap-1 text-xs text-muted-foreground">
              Options <DockerDocsLink anchor="dns-option" />
            </label>
            <MultiCombobox
              values={resolverOptions}
              onChange={setResolverOptions}
              options={[]}
              placeholder="Type an option and press Enter..."
            />
          </div>
        </>
      }
    />
  );
}
