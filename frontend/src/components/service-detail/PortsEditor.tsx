import { api } from "@/api/client";
import type { PortConfig } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import { DockerDocsLink } from "@/components/service-detail/DockerDocsLink";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
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

export function PortsEditor({
  serviceId,
  ports,
  onSaved,
  canEdit = false,
}: PortsEditorProps & { canEdit?: boolean }) {

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [draft, setDraft] = useState<PortConfig[]>([]);
  useEscapeCancel(editing, () => cancelEdit());

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

  const controls =
    !editing && canEdit ? (
      <Button
        variant="outline"
        size="xs"
        onClick={(event: React.MouseEvent) => {
          event.stopPropagation();
          openEdit();
        }}
      >
        <Pencil className="size-3" />
        Edit
      </Button>
    ) : undefined;

  return (
    <CollapsibleSection
      title="Published Ports"
      defaultOpen={ports.length > 0}
      controls={controls}
    >
      {editing ? (
        <div className="space-y-3 rounded-lg border p-3">
          {draft.length === 0 ? (
            <button
              type="button"
              onClick={addPort}
              className="flex w-full flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground transition-colors hover:border-muted-foreground/40 hover:text-foreground"
            >
              <Plus className="size-4" />
              <p className="text-sm">Add a port mapping</p>
            </button>
          ) : (
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
              {draft.map((port, index) => (
                <div
                  key={index}
                  className="relative rounded-lg border p-3"
                >
                  <Button
                    variant="outline"
                    size="xs"
                    className="absolute top-2 right-2"
                    onClick={() => removePort(index)}
                  >
                    <Trash2 className="size-3" />
                  </Button>

                  <div className="grid grid-cols-2 gap-3 pe-10">
                    <div className="flex flex-col gap-1.5">
                      <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                        Protocol{" "}
                        <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#publish" />
                      </label>

                      <select
                        value={port.Protocol}
                        onChange={(event) => updatePort(index, "Protocol", event.target.value)}
                        className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                      >
                        <option value="tcp">TCP</option>
                        <option value="udp">UDP</option>
                        <option value="sctp">SCTP</option>
                      </select>
                    </div>

                    <div className="flex flex-col gap-1.5">
                      <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                        Publish mode{" "}
                        <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#publish" />
                      </label>

                      <select
                        value={port.PublishMode}
                        onChange={(event) => updatePort(index, "PublishMode", event.target.value)}
                        className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                      >
                        <option value="ingress">Ingress (load-balanced)</option>
                        <option value="host">Host (direct)</option>
                      </select>
                    </div>

                    <div className="flex flex-col gap-1.5">
                      <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                        Container port{" "}
                        <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#publish" />
                      </label>

                      <Input
                        type="number"
                        min={1}
                        max={65535}
                        value={port.TargetPort || ""}
                        onChange={(event) =>
                          updatePort(index, "TargetPort", Number(event.target.value) || 0)
                        }
                        placeholder="80"
                      />
                    </div>

                    <div className="flex flex-col gap-1.5">
                      <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                        Host port{" "}
                        <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#publish" />
                      </label>

                      <Input
                        type="number"
                        min={0}
                        max={65535}
                        value={port.PublishedPort || ""}
                        onChange={(event) =>
                          updatePort(index, "PublishedPort", Number(event.target.value) || 0)
                        }
                        placeholder="Auto-assign"
                      />
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

          <footer className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={addPort}
            >
              <Plus className="size-3" />
              Add port
            </Button>

            <div className="ms-auto flex gap-2">
              <Button
                size="sm"
                onClick={save}
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
            </div>
          </footer>
        </div>
      ) : ports.length === 0 ? (
        <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
          <p className="text-sm">No published ports</p>
          {canEdit && (
            <p className="text-xs">
              Click Edit to expose container ports to the swarm routing mesh.
            </p>
          )}
        </div>
      ) : (
        <div className="flex flex-wrap gap-2 rounded-lg border p-3">
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
    </CollapsibleSection>
  );
}
