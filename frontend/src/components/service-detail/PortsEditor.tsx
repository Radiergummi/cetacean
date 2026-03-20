import { api } from "@/api/client";
import type { PortConfig } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

interface PortsEditorProps {
  serviceId: string;
  ports: PortConfig[];
  onSaved: (ports: PortConfig[]) => void;
}

const defaultPort: PortConfig = {
  Protocol: "tcp",
  TargetPort: 0,
  PublishedPort: 0,
  PublishMode: "ingress",
};

export function PortsEditor({ serviceId, ports, onSaved }: PortsEditorProps) {
  const { level } = useOperationsLevel();
  const canEdit = level >= 1;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [draft, setDraft] = useState<PortConfig[]>([]);

  function openEdit() {
    setDraft(ports.map((port) => ({ ...port })));
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addPort() {
    setDraft([...draft, { ...defaultPort }]);
  }

  function removePort(index: number) {
    setDraft(draft.filter((_, i) => i !== index));
  }

  function updatePort(index: number, field: keyof PortConfig, value: string | number) {
    const updated = [...draft];
    updated[index] = { ...updated[index], [field]: value };
    setDraft(updated);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.patchServicePorts(serviceId, draft);
      setEditing(false);
      onSaved(result.ports);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update ports"));
    } finally {
      setSaving(false);
    }
  }

  if (!editing) {
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Published Ports
          </h3>

          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
            disabled={!canEdit}
            title={canEdit ? undefined : "Editing disabled by server configuration"}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        </div>

        {ports.length === 0 ? (
          <p className="text-sm text-muted-foreground">No published ports.</p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {ports.map(({ Protocol, PublishMode, PublishedPort, TargetPort }, index) => (
              <span
                key={index}
                className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-2 font-mono text-sm"
              >
                <span className="font-semibold">{PublishedPort || "auto"}</span>
                <span className="text-muted-foreground">{"\u2192"}</span>
                <span>
                  {TargetPort}/{Protocol}
                </span>
                <span className="text-xs text-muted-foreground">({PublishMode})</span>
              </span>
            ))}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        Published Ports
      </h3>

      <div className="space-y-3">
        {draft.map((port, index) => (
          <div key={index} className="relative rounded-lg border p-3">
            <Button
              variant="outline"
              size="xs"
              className="absolute top-2 right-2"
              onClick={() => removePort(index)}
            >
              <Trash2 className="size-3" />
            </Button>

            <div className="grid grid-cols-2 gap-3 pr-10">
              <div className="space-y-1">
                <label className="text-xs font-medium text-muted-foreground">Protocol</label>

                <select
                  value={port.Protocol}
                  onChange={(event) => updatePort(index, "Protocol", event.target.value)}
                  className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm"
                >
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                  <option value="sctp">SCTP</option>
                </select>
              </div>

              <div className="space-y-1">
                <label className="text-xs font-medium text-muted-foreground">Publish mode</label>

                <select
                  value={port.PublishMode}
                  onChange={(event) => updatePort(index, "PublishMode", event.target.value)}
                  className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm"
                >
                  <option value="ingress">Ingress (load-balanced)</option>
                  <option value="host">Host (direct)</option>
                </select>
              </div>

              <div className="space-y-1">
                <label className="text-xs font-medium text-muted-foreground">Container port</label>

                <Input
                  type="number"
                  min={1}
                  max={65535}
                  value={port.TargetPort || ""}
                  onChange={(event) => updatePort(index, "TargetPort", Number(event.target.value) || 0)}
                  placeholder="80"
                />
              </div>

              <div className="space-y-1">
                <label className="text-xs font-medium text-muted-foreground">Host port</label>

                <Input
                  type="number"
                  min={0}
                  max={65535}
                  value={port.PublishedPort || ""}
                  onChange={(event) => updatePort(index, "PublishedPort", Number(event.target.value) || 0)}
                  placeholder="Auto-assign"
                />
              </div>
            </div>
          </div>
        ))}
      </div>

      <Button variant="outline" size="sm" onClick={addPort}>
        <Plus className="size-3" />
        Add port
      </Button>

      {saveError && <p className="text-xs text-red-600">{saveError}</p>}

      <div className="flex gap-2">
        <Button size="sm" onClick={save} disabled={saving}>
          {saving && <Spinner className="size-3" />}
          Save
        </Button>

        <Button variant="outline" size="sm" onClick={cancelEdit} disabled={saving}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
