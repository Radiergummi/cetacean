import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { MultiCombobox } from "@/components/ui/multi-combobox";
import { useState } from "react";

const linuxCapabilities = [
  { value: "ALL", label: "ALL", description: "All capabilities" },
  { value: "NET_ADMIN", label: "NET_ADMIN", description: "Network administration" },
  { value: "NET_RAW", label: "NET_RAW", description: "Raw network access" },
  { value: "NET_BIND_SERVICE", label: "NET_BIND_SERVICE", description: "Bind to privileged ports" },
  { value: "SYS_ADMIN", label: "SYS_ADMIN", description: "System administration" },
  { value: "SYS_PTRACE", label: "SYS_PTRACE", description: "Trace processes" },
  { value: "SYS_TIME", label: "SYS_TIME", description: "Set system clock" },
  { value: "SYS_RESOURCE", label: "SYS_RESOURCE", description: "Override resource limits" },
  { value: "IPC_LOCK", label: "IPC_LOCK", description: "Lock memory" },
  { value: "CHOWN", label: "CHOWN", description: "Change file ownership" },
  { value: "DAC_OVERRIDE", label: "DAC_OVERRIDE", description: "Bypass file permissions" },
  { value: "FOWNER", label: "FOWNER", description: "Bypass ownership checks" },
  { value: "SETUID", label: "SETUID", description: "Set user ID" },
  { value: "SETGID", label: "SETGID", description: "Set group ID" },
  { value: "MKNOD", label: "MKNOD", description: "Create device files" },
  { value: "AUDIT_WRITE", label: "AUDIT_WRITE", description: "Write to audit log" },
  { value: "KILL", label: "KILL", description: "Send signals to processes" },
];

function CapabilityBadges({ items }: { items: string[] }) {
  return (
    <div className="flex flex-wrap gap-1">
      {items.map((cap) => (
        <span
          key={cap}
          className="inline-flex items-center rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
        >
          {cap}
        </span>
      ))}
    </div>
  );
}

export function CapabilitiesEditor({
  serviceId,
  config,
  onSaved,
}: {
  serviceId: string;
  config: ContainerConfig;
  onSaved: (updated: ContainerConfig) => void;
}) {
  const [addList, setAddList] = useState<string[]>([]);
  const [dropList, setDropList] = useState<string[]>([]);

  const isEmpty = !config.capabilityAdd?.length && !config.capabilityDrop?.length;

  function resetForm() {
    setAddList(config.capabilityAdd ?? []);
    setDropList(config.capabilityDrop ?? []);
  }

  async function save() {
    const updated = await api.patchServiceContainerConfig(serviceId, {
      capabilityAdd: addList.length > 0 ? addList : null,
      capabilityDrop: dropList.length > 0 ? dropList : null,
    });
    onSaved(updated);
  }

  return (
    <EditablePanel
      title="Capabilities"
      empty={isEmpty}
      emptyDescription="Click Edit to add or drop Linux capabilities."
      onOpen={resetForm}
      onSave={save}
      display={
        <dl className="grid gap-y-2 text-sm">
          {config.capabilityAdd && config.capabilityAdd.length > 0 && (
            <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
              <dt className="text-muted-foreground">Add</dt>
              <dd>
                <CapabilityBadges items={config.capabilityAdd} />
              </dd>
            </div>
          )}

          {config.capabilityDrop && config.capabilityDrop.length > 0 && (
            <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
              <dt className="text-muted-foreground">Drop</dt>
              <dd>
                <CapabilityBadges items={config.capabilityDrop} />
              </dd>
            </div>
          )}
        </dl>
      }
      edit={
        <>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs text-muted-foreground">Add Capabilities</label>
            <MultiCombobox
              values={addList}
              onChange={setAddList}
              options={linuxCapabilities}
              placeholder="Select or type a capability..."
              transformInput={(value) => value.toUpperCase()}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-xs text-muted-foreground">Drop Capabilities</label>
            <MultiCombobox
              values={dropList}
              onChange={setDropList}
              options={linuxCapabilities}
              placeholder="Select or type a capability..."
              transformInput={(value) => value.toUpperCase()}
            />
          </div>
        </>
      }
    />
  );
}
