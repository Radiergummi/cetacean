import { api } from "@/api/client";
import type { ServiceSecretRef } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import SimpleTable from "@/components/SimpleTable";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Combobox } from "@/components/ui/combobox";
import { Input } from "@/components/ui/input";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";

interface SecretsEditorProps {
  serviceId: string;
  secrets: ServiceSecretRef[];
  onSaved: (secrets: ServiceSecretRef[]) => void;
}

interface SecretOption {
  value: string;
  label: string;
  description?: string;
}

export function SecretsEditor({ serviceId, secrets, onSaved }: SecretsEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [draft, setDraft] = useState<ServiceSecretRef[]>([]);
  const [availableSecrets, setAvailableSecrets] = useState<SecretOption[]>([]);
  const [adding, setAdding] = useState(false);

  const [newSecretId, setNewSecretId] = useState("");
  const [newTargetPath, setNewTargetPath] = useState("");

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setDraft(secrets.map((secret) => ({ ...secret })));
    setSaveError(null);
    setNewSecretId("");
    setNewTargetPath("");
    setAdding(secrets.length === 0);
    setEditing(true);
  }

  useEffect(() => {
    if (!editing) {
      return;
    }

    let cancelled = false;

    api
      .secrets({ limit: 0 })
      .then((response) => {
        if (!cancelled) {
          setAvailableSecrets(
            response.items.map((secret) => ({
              value: secret.ID,
              label: secret.Spec.Name,
              description: secret.ID.slice(0, 12),
            })),
          );
        }
      })
      .catch(() => {});

    return () => {
      cancelled = true;
    };
  }, [editing]);

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function removeSecret(index: number) {
    setDraft(draft.filter((_, i) => i !== index));
  }

  function handleSecretSelected(secretId: string) {
    setNewSecretId(secretId);

    const match = availableSecrets.find((option) => option.value === secretId);

    if (match) {
      setNewTargetPath(`/run/secrets/${match.label}`);
    }
  }

  function addRow() {
    if (!newSecretId || !newTargetPath) {
      return;
    }

    const match = availableSecrets.find((option) => option.value === newSecretId);
    const secretName = match ? match.label : newSecretId;

    setDraft((previous) => [
      ...previous,
      { secretID: newSecretId, secretName, fileName: newTargetPath },
    ]);
    setNewSecretId("");
    setNewTargetPath("");
    setAdding(false);
  }

  async function save() {
    const effectiveDraft =
      newSecretId && newTargetPath
        ? [
            ...draft,
            {
              secretID: newSecretId,
              secretName:
                availableSecrets.find((option) => option.value === newSecretId)?.label ??
                newSecretId,
              fileName: newTargetPath,
            },
          ]
        : draft;

    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.patchServiceSecrets(serviceId, effectiveDraft);
      setEditing(false);
      onSaved(result.secrets);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Failed to update secrets"));
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
      title="Secrets"
      defaultOpen={secrets.length > 0}
      controls={controls}
    >
      {editing ? (
        <div className="flex flex-col gap-3">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-max whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-background/50">
                <tr className="bg-muted/50 dark:bg-transparent">
                  <th className="ps-3 pt-3 text-left text-sm font-medium">Secret</th>
                  <th className="ps-3 pt-3 text-left text-sm font-medium">Target</th>
                  <th className="w-12 py-3 ps-3" />
                </tr>
              </thead>
              <tbody>
                {draft.map(({ secretID, secretName, fileName }, index) => (
                  <tr
                    key={secretID}
                    className="border-b bg-transparent! last:border-b-0"
                  >
                    <td className="py-3 ps-3 text-sm">{secretName}</td>
                    <td className="py-3 ps-3 font-mono text-sm">{fileName}</td>
                    <td className="py-3 ps-3">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => removeSecret(index)}
                        title="Remove"
                        className="text-muted-foreground hover:text-red-600"
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </td>
                  </tr>
                ))}
                {adding && (
                  <tr className="bg-transparent!">
                    <td className="py-3 ps-3">
                      <Combobox
                        value={newSecretId}
                        onChange={handleSecretSelected}
                        options={availableSecrets}
                        placeholder="Select secret..."
                        allowCustom={false}
                      />
                    </td>
                    <td className="py-3 ps-3">
                      <Input
                        value={newTargetPath}
                        onChange={(event) => setNewTargetPath(event.target.value)}
                        placeholder="/run/secrets/my-secret"
                        onKeyDown={(event) => {
                          if (event.key === "Enter" && newSecretId && newTargetPath) {
                            addRow();
                          }
                        }}
                      />
                    </td>
                    <td />
                  </tr>
                )}
              </tbody>
            </table>

            {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

            <footer className="flex items-center gap-2 p-3">
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  if (adding && newSecretId && newTargetPath) addRow();
                  setAdding(true);
                }}
                disabled={adding && (!newSecretId || !newTargetPath)}
              >
                Add another
              </Button>
              <div className="ml-auto flex gap-2">
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
              </div>
            </footer>
          </div>
        </div>
      ) : secrets.length === 0 ? (
        <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
          <p className="text-sm">No secrets attached</p>
          {canEdit && (
            <p className="text-xs">Click Edit to attach Docker secrets to this service.</p>
          )}
        </div>
      ) : (
        <SimpleTable
          columns={["Name", "Target"]}
          items={secrets}
          keyFn={({ secretID }) => secretID}
          renderRow={({ secretID, secretName, fileName }) => (
            <>
              <td className="p-3 text-sm">
                <Link
                  to={`/secrets/${secretID}`}
                  className="text-link hover:underline"
                >
                  {secretName}
                </Link>
              </td>
              <td className="p-3 font-mono text-sm">{fileName}</td>
            </>
          )}
        />
      )}
    </CollapsibleSection>
  );
}
