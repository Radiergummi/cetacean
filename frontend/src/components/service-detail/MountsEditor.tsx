import { api } from "@/api/client";
import type { ServiceMount } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import ResourceName from "@/components/ResourceName";
import { DockerDocsLink } from "@/components/service-detail/DockerDocsLink";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { getErrorMessage } from "@/lib/utils";
import { ArrowRight, Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";

interface MountsEditorProps {
  serviceId: string;
  mounts: ServiceMount[];
  onSaved: (mounts: ServiceMount[]) => void;
}

const defaultMount: ServiceMount = {
  Type: "volume",
  Source: "",
  Target: "",
};

const mountTypes = ["bind", "volume", "tmpfs", "npipe", "cluster", "image"] as const;

const propagationOptions = ["private", "rprivate", "shared", "rshared", "slave", "rslave"] as const;

const selectClassName =
  "flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50";

function sourceLabel(type: string): string {
  switch (type) {
    case "bind":
      return "Host path";
    case "volume":
      return "Volume name";
    case "npipe":
      return "Pipe name";
    case "cluster":
      return "CSI volume";
    case "image":
      return "Image";
    default:
      return "Source";
  }
}

export function MountsEditor({
  serviceId,
  mounts,
  onSaved,
  canEdit = false,
}: MountsEditorProps & { canEdit?: boolean }) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [draft, setDraft] = useState<ServiceMount[]>([]);
  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setDraft(mounts.map((mount) => structuredClone(mount)));
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addMount() {
    setDraft([...draft, { ...defaultMount }]);
  }

  function removeMount(index: number) {
    setDraft(draft.filter((_, filterIndex) => filterIndex !== index));
  }

  function updateMount(index: number, updated: ServiceMount) {
    const next = [...draft];
    next[index] = updated;
    setDraft(next);
  }

  function handleTypeChange(index: number, newType: string) {
    const mount = draft[index];
    const updated: ServiceMount = {
      Type: newType,
      Source: newType === "tmpfs" ? "" : mount.Source,
      Target: mount.Target,
      ReadOnly: mount.ReadOnly,
    };

    updateMount(index, updated);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.patchServiceMounts(serviceId, draft);
      setEditing(false);
      onSaved(result.mounts);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update mounts"));
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
      title="Mounts"
      defaultOpen={mounts.length > 0}
      controls={controls}
    >
      {editing ? (
        <div className="space-y-3 rounded-lg border p-3">
          {draft.length === 0 ? (
            <button
              type="button"
              onClick={addMount}
              className="flex w-full flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground transition-colors hover:border-muted-foreground/40 hover:text-foreground"
            >
              <Plus className="size-4" />
              <p className="text-sm">Add a mount</p>
            </button>
          ) : (
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
              {draft.map((mount, index) => (
                <div
                  key={index}
                  className="relative rounded-lg border p-3"
                >
                  <Button
                    variant="outline"
                    size="xs"
                    className="absolute top-2 right-2"
                    onClick={() => removeMount(index)}
                  >
                    <Trash2 className="size-3" />
                  </Button>

                  <div className="grid grid-cols-2 gap-3 pe-10">
                    <div className="flex flex-col gap-1.5">
                      <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                        Type <DockerDocsLink href="https://docs.docker.com/engine/storage/" />
                      </label>

                      <select
                        value={mount.Type}
                        onChange={(event) => handleTypeChange(index, event.target.value)}
                        className={selectClassName}
                      >
                        {mountTypes.map((type) => (
                          <option
                            key={type}
                            value={type}
                          >
                            {type}
                          </option>
                        ))}
                      </select>
                    </div>

                    {mount.Type !== "tmpfs" && (
                      <div className="flex flex-col gap-1.5">
                        <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                          {sourceLabel(mount.Type)}
                        </label>

                        <Input
                          value={mount.Source}
                          onChange={(event) =>
                            updateMount(index, {
                              ...mount,
                              Source: event.target.value,
                            })
                          }
                          placeholder={mount.Type === "bind" ? "/host/path" : ""}
                        />
                      </div>
                    )}

                    <div className="flex flex-col gap-1.5">
                      <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                        Container path
                      </label>

                      <Input
                        value={mount.Target}
                        onChange={(event) =>
                          updateMount(index, {
                            ...mount,
                            Target: event.target.value,
                          })
                        }
                        placeholder="/container/path"
                      />
                    </div>

                    <div className="flex flex-col gap-1.5">
                      <label className="flex items-center gap-2 text-xs font-medium text-foreground">
                        <input
                          type="checkbox"
                          checked={mount.ReadOnly ?? false}
                          onChange={(event) =>
                            updateMount(index, {
                              ...mount,
                              ReadOnly: event.target.checked,
                            })
                          }
                        />
                        Read-only
                      </label>
                    </div>

                    {mount.Type === "bind" && (
                      <div className="flex flex-col gap-1.5">
                        <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                          Propagation
                        </label>

                        <select
                          value={mount.BindOptions?.Propagation ?? "rprivate"}
                          onChange={(event) =>
                            updateMount(index, {
                              ...mount,
                              BindOptions: {
                                ...mount.BindOptions,
                                Propagation: event.target.value,
                              },
                            })
                          }
                          className={selectClassName}
                        >
                          {propagationOptions.map((option) => (
                            <option
                              key={option}
                              value={option}
                            >
                              {option}
                            </option>
                          ))}
                        </select>
                      </div>
                    )}

                    {mount.Type === "volume" && (
                      <>
                        <div className="flex flex-col gap-1.5">
                          <label className="flex items-center gap-2 text-xs font-medium text-foreground">
                            <input
                              type="checkbox"
                              checked={mount.VolumeOptions?.NoCopy ?? false}
                              onChange={(event) =>
                                updateMount(index, {
                                  ...mount,
                                  VolumeOptions: {
                                    ...mount.VolumeOptions,
                                    NoCopy: event.target.checked,
                                  },
                                })
                              }
                            />
                            No-copy
                          </label>
                        </div>

                        <div className="flex flex-col gap-1.5">
                          <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                            Subpath
                          </label>

                          <Input
                            value={mount.VolumeOptions?.Subpath ?? ""}
                            onChange={(event) =>
                              updateMount(index, {
                                ...mount,
                                VolumeOptions: {
                                  ...mount.VolumeOptions,
                                  Subpath: event.target.value || undefined,
                                },
                              })
                            }
                            placeholder="Optional subpath"
                          />
                        </div>
                      </>
                    )}

                    {mount.Type === "tmpfs" && (
                      <>
                        <div className="flex flex-col gap-1.5">
                          <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                            Size (bytes)
                          </label>

                          <Input
                            type="number"
                            min={0}
                            value={mount.TmpfsOptions?.SizeBytes ?? ""}
                            onChange={(event) =>
                              updateMount(index, {
                                ...mount,
                                TmpfsOptions: {
                                  ...mount.TmpfsOptions,
                                  SizeBytes: Number(event.target.value) || undefined,
                                },
                              })
                            }
                            placeholder="0 (unlimited)"
                          />
                        </div>

                        <div className="flex flex-col gap-1.5">
                          <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                            Mode (octal)
                          </label>

                          <Input
                            type="number"
                            min={0}
                            value={mount.TmpfsOptions?.Mode ?? ""}
                            onChange={(event) =>
                              updateMount(index, {
                                ...mount,
                                TmpfsOptions: {
                                  ...mount.TmpfsOptions,
                                  Mode: Number(event.target.value) || undefined,
                                },
                              })
                            }
                            placeholder="1777"
                          />
                        </div>
                      </>
                    )}

                    {mount.Type === "image" && (
                      <div className="flex flex-col gap-1.5">
                        <label className="flex items-center gap-1 text-xs font-medium text-foreground">
                          Subpath
                        </label>

                        <Input
                          value={mount.ImageOptions?.Subpath ?? ""}
                          onChange={(event) =>
                            updateMount(index, {
                              ...mount,
                              ImageOptions: {
                                ...mount.ImageOptions,
                                Subpath: event.target.value || undefined,
                              },
                            })
                          }
                          placeholder="Optional subpath"
                        />
                      </div>
                    )}
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
              onClick={addMount}
            >
              <Plus className="size-3" />
              Add mount
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
      ) : mounts.length === 0 ? (
        <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
          <p className="text-sm">No mounts configured</p>
          {canEdit && (
            <p className="text-xs">Click Edit to add bind mounts, volumes, or tmpfs mounts.</p>
          )}
        </div>
      ) : (
        <div className="flex flex-wrap gap-2 rounded-lg border p-3">
          {mounts.map(({ Type, Source, Target, ReadOnly }, index) => (
            <span
              key={index}
              className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-2 text-sm"
            >
              <MountTypeBadge type={Type} />
              {Type === "tmpfs" ? (
                <span className="font-mono">{Target}</span>
              ) : (
                <>
                  {Type === "volume" && Source ? (
                    <Link
                      to={`/volumes/${Source}`}
                      className="font-mono text-link hover:underline"
                    >
                      <ResourceName name={Source} />
                    </Link>
                  ) : (
                    <span className="font-mono">{Source}</span>
                  )}
                  <ArrowRight className="size-3.5 text-muted-foreground" />
                  <span className="font-mono">{Target}</span>
                </>
              )}
              {ReadOnly && (
                <span className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                  ro
                </span>
              )}
            </span>
          ))}
        </div>
      )}
    </CollapsibleSection>
  );
}

function MountTypeBadge({ type }: { type: string }) {
  return (
    <span
      data-type={type}
      className="inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium data-[type=bind]:bg-amber-100 data-[type=bind]:text-amber-800 data-[type=cluster]:bg-teal-100 data-[type=cluster]:text-teal-800 data-[type=image]:bg-indigo-100 data-[type=image]:text-indigo-800 data-[type=npipe]:bg-slate-100 data-[type=npipe]:text-slate-800 data-[type=tmpfs]:bg-purple-100 data-[type=tmpfs]:text-purple-800 data-[type=volume]:bg-blue-100 data-[type=volume]:text-blue-800 dark:data-[type=bind]:bg-amber-900/30 dark:data-[type=bind]:text-amber-300 dark:data-[type=cluster]:bg-teal-900/30 dark:data-[type=cluster]:text-teal-300 dark:data-[type=image]:bg-indigo-900/30 dark:data-[type=image]:text-indigo-300 dark:data-[type=npipe]:bg-slate-700/30 dark:data-[type=npipe]:text-slate-300 dark:data-[type=tmpfs]:bg-purple-900/30 dark:data-[type=tmpfs]:text-purple-300 dark:data-[type=volume]:bg-blue-900/30 dark:data-[type=volume]:text-blue-300"
    >
      {type}
    </span>
  );
}
