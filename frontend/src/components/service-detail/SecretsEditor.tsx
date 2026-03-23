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
import { Pencil, Plus, Trash2 } from "lucide-react";
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

  const [newSecretId, setNewSecretId] = useState("");
  const [newTargetPath, setNewTargetPath] = useState("");

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setDraft(secrets.map((secret) => ({ ...secret })));
    setSaveError(null);
    setNewSecretId("");
    setNewTargetPath("");
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

  function addSecret() {
    if (!newSecretId || !newTargetPath) {
      return;
    }

    const match = availableSecrets.find((option) => option.value === newSecretId);
    const secretName = match ? match.label : newSecretId;

    setDraft([...draft, { secretID: newSecretId, secretName, fileName: newTargetPath }]);
    setNewSecretId("");
    setNewTargetPath("");
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.patchServiceSecrets(serviceId, draft);
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
        <div className="space-y-3 rounded-lg border p-3">
          {draft.length > 0 && (
            <SimpleTable
              columns={["Name", "Target", ""]}
              items={draft}
              keyFn={(_, index) => index}
              renderRow={({ secretName, fileName }, index) => (
                <>
                  <td className="p-3 text-sm">{secretName}</td>
                  <td className="p-3 font-mono text-sm">{fileName}</td>
                  <td className="p-3 text-right">
                    <Button
                      variant="outline"
                      size="xs"
                      onClick={() => removeSecret(index)}
                    >
                      <Trash2 className="size-3" />
                    </Button>
                  </td>
                </>
              )}
            />
          )}

          <div className="flex items-end gap-2 border-t border-dashed pt-3">
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <label className="text-xs font-medium text-foreground">Secret</label>

              <Combobox
                value={newSecretId}
                onChange={handleSecretSelected}
                options={availableSecrets}
                placeholder="Select secret..."
                allowCustom={false}
              />
            </div>

            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <label className="text-xs font-medium text-foreground">Target path</label>

              <Input
                value={newTargetPath}
                onChange={(event) => setNewTargetPath(event.target.value)}
                placeholder="/run/secrets/my-secret"
              />
            </div>

            <Button
              variant="outline"
              size="sm"
              onClick={addSecret}
              disabled={!newSecretId || !newTargetPath}
            >
              <Plus className="size-3" />
              Add
            </Button>
          </div>

          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

          <footer className="flex items-center gap-2">
            <div className="ml-auto flex gap-2">
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
