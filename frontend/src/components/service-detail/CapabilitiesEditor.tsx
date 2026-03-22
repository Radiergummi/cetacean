import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { ContainerConfig } from "@/api/types";
import { Input } from "@/components/ui/input";
import { X } from "lucide-react";
import type { KeyboardEvent } from "react";
import { useState } from "react";

function CapabilityBadges({
  items,
  onRemove,
}: {
  items: string[];
  onRemove?: (cap: string) => void;
}) {
  if (items.length === 0) {
    return <span className="text-muted-foreground">None</span>;
  }

  return (
    <div className="flex flex-wrap gap-1">
      {items.map((cap) => (
        <span
          key={cap}
          className="inline-flex items-center gap-1 rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
        >
          {cap}
          {onRemove && (
            <button
              type="button"
              onClick={() => onRemove(cap)}
              className="text-muted-foreground hover:text-foreground"
              aria-label={`Remove ${cap}`}
            >
              <X className="size-3" />
            </button>
          )}
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
  const [addInput, setAddInput] = useState("");
  const [dropInput, setDropInput] = useState("");

  function resetForm() {
    setAddList(config.capabilityAdd ?? []);
    setDropList(config.capabilityDrop ?? []);
    setAddInput("");
    setDropInput("");
  }

  function handleKeyDown(
    event: KeyboardEvent<HTMLInputElement>,
    input: string,
    list: string[],
    setList: (list: string[]) => void,
    setInput: (value: string) => void,
  ) {
    if (event.key !== "Enter") {
      return;
    }

    event.preventDefault();

    const value = input.trim().toUpperCase();

    if (value && !list.includes(value)) {
      setList([...list, value]);
    }

    setInput("");
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
      onOpen={resetForm}
      onSave={save}
      display={
        <dl className="grid gap-y-2 text-sm">
          <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
            <dt className="text-muted-foreground">Add</dt>
            <dd>
              <CapabilityBadges items={config.capabilityAdd ?? []} />
            </dd>
          </div>

          <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
            <dt className="text-muted-foreground">Drop</dt>
            <dd>
              <CapabilityBadges items={config.capabilityDrop ?? []} />
            </dd>
          </div>
        </dl>
      }
      edit={
        <>
          <div className="flex flex-col gap-2">
            <label className="text-xs text-muted-foreground">Add Capabilities</label>
            <CapabilityBadges
              items={addList}
              onRemove={(cap) => setAddList(addList.filter((c) => c !== cap))}
            />
            <Input
              value={addInput}
              onChange={(event) => setAddInput(event.target.value.toUpperCase())}
              onKeyDown={(event) =>
                handleKeyDown(event, addInput, addList, setAddList, setAddInput)
              }
              placeholder="NET_ADMIN — press Enter to add"
              className="font-mono"
            />
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-xs text-muted-foreground">Drop Capabilities</label>
            <CapabilityBadges
              items={dropList}
              onRemove={(cap) => setDropList(dropList.filter((c) => c !== cap))}
            />
            <Input
              value={dropInput}
              onChange={(event) => setDropInput(event.target.value.toUpperCase())}
              onKeyDown={(event) =>
                handleKeyDown(event, dropInput, dropList, setDropList, setDropInput)
              }
              placeholder="ALL — press Enter to add"
              className="font-mono"
            />
          </div>
        </>
      }
    />
  );
}
