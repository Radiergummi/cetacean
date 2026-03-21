import type { PatchOp } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import KeyValuePills from "@/components/data/KeyValuePills";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Trash2 } from "lucide-react";
import type React from "react";
import { useMemo, useState } from "react";

interface KeyValueEditorProps {
  title: string;
  entries: Record<string, string>;
  keyLabel?: string;
  valueLabel?: string;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  onSave: (ops: PatchOp[]) => Promise<Record<string, string>>;
  defaultOpen?: boolean;
  renderValue?: (value: string) => React.ReactNode;
  onCopyValue?: React.ClipboardEventHandler;
  editDisabled?: boolean;
  /** Return true if this key should be read-only (no edit, no delete). */
  isKeyReadOnly?: (key: string) => boolean;
  /** Validate a new key. Return an error message, or null if valid. */
  validateKey?: (key: string) => string | null;
}

export function KeyValueEditor({
  title,
  entries,
  keyLabel = "Key",
  valueLabel = "Value",
  keyPlaceholder = "key",
  valuePlaceholder = "value",
  onSave,
  defaultOpen = false,
  renderValue,
  onCopyValue,
  editDisabled = false,
  isKeyReadOnly,
  validateKey,
}: KeyValueEditorProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [adding, setAdding] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setDraft({ ...entries });
    setNewKey("");
    setNewValue("");
    setAdding(Object.keys(entries).length === 0);
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addRow() {
    const key = newKey.trim();
    if (!key) return;
    setDraft((previous) => ({ ...previous, [key]: newValue }));
    setNewKey("");
    setNewValue("");
    setAdding(false);
  }

  // Intentional: no confirmation on row removal. Removals are draft-only
  // and not persisted until Save. The user can always Cancel to undo.
  // This replaces the previous window.confirm() per spec decision.
  function removeRow(key: string) {
    setDraft((previous) => {
      const next = { ...previous };
      delete next[key];
      return next;
    });
  }

  const newKeyError = newKey.trim() ? validateKey?.(newKey.trim()) ?? null : null;

  async function save() {
    const ops: PatchOp[] = [];
    const trimmedKey = newKey.trim();
    const effectiveDraft =
      trimmedKey && !newKeyError ? { ...draft, [trimmedKey]: newValue } : draft;

    for (const key of Object.keys(entries)) {
      if (isKeyReadOnly?.(key)) {
        continue;
      }

      if (!(key in effectiveDraft)) {
        ops.push({ op: "remove", path: `/${key}` });
      }
    }

    for (const [key, value] of Object.entries(effectiveDraft)) {
      if (isKeyReadOnly?.(key)) {
        continue;
      }

      if (!(key in entries)) {
        ops.push({ op: "add", path: `/${key}`, value });
      } else if (entries[key] !== value) {
        ops.push({ op: "replace", path: `/${key}`, value });
      }
    }
    if (ops.length === 0) {
      setEditing(false);
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      await onSave(ops);
      setEditing(false);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Save failed"));
    } finally {
      setSaving(false);
    }
  }

  const sortedEntries = useMemo(
    () => Object.entries(entries).sort(([a], [b]) => a.localeCompare(b)),
    [entries],
  );
  const draftEntries = useMemo(
    () => Object.entries(draft).sort(([a], [b]) => a.localeCompare(b)),
    [draft],
  );

  const controls = !editing && !editDisabled ? (
    <Button
      variant="outline"
      size="xs"
      onClick={openEdit}
    >
      <Pencil className="size-3" />
      Edit
    </Button>
  ) : null;

  return (
    <CollapsibleSection
      title={title}
      defaultOpen={defaultOpen}
      controls={controls}
    >
      {!editing ? (
        sortedEntries.length === 0 ? (
          <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
            <p className="text-sm">No {title.toLowerCase()}</p>
            {!editDisabled && <p className="text-xs">Click Edit to add entries.</p>}
          </div>
        ) : (
          <KeyValuePills
            entries={sortedEntries}
            renderValue={renderValue}
            onCopy={onCopyValue}
          />
        )
      ) : (
        <div className="flex flex-col gap-3">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-max whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-background/50">
                <tr className="bg-muted/50 dark:bg-transparent">
                  <th className="pt-3 ps-3 text-left text-sm font-medium">{keyLabel}</th>
                  <th className="pt-3 ps-3 text-left text-sm font-medium">{valueLabel}</th>
                  <th className="w-12 py-3 ps-3" />
                </tr>
              </thead>
              <tbody>
                {draftEntries.map(([key, value]) => {
                  const readOnly = isKeyReadOnly?.(key) ?? false;

                  return (
                    <tr
                      key={key}
                      className="border-b last:border-b-0 bg-transparent!"
                    >
                      <td className="py-3 ps-3 font-mono text-xs">
                        {key}
                        {readOnly && (
                          <span className="ml-2 rounded bg-muted px-1.5 py-0.5 font-sans text-[10px] text-muted-foreground">
                            read-only
                          </span>
                        )}
                      </td>
                      <td className="py-3 ps-3">
                        {readOnly ? (
                          <span className="font-mono text-xs text-muted-foreground">{value}</span>
                        ) : (
                          <Input
                            value={value}
                            onChange={(event) =>
                              setDraft((previous) => ({
                                ...previous,
                                [key]: event.target.value,
                              }))
                            }
                            className="font-mono text-xs"
                          />
                        )}
                      </td>
                      <td className="py-3 ps-3">
                        {!readOnly && (
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            onClick={() => removeRow(key)}
                            title="Remove"
                            className="text-muted-foreground hover:text-red-600"
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        )}
                      </td>
                    </tr>
                  );
                })}
                {adding && (
                  <>
                    <tr className="bg-transparent!">
                      <td className="py-3 ps-3">
                        <Input
                          value={newKey}
                          onChange={(event) => setNewKey(event.target.value)}
                          placeholder={keyPlaceholder}
                          className="font-mono text-xs"
                          autoFocus
                        />
                      </td>
                      <td className="py-3 ps-3">
                        <Input
                          value={newValue}
                          onChange={(event) => setNewValue(event.target.value)}
                          placeholder={valuePlaceholder}
                          onKeyDown={(event) => {
                            if (event.key === "Enter" && !newKeyError) {
                              addRow();
                            }
                          }}
                          className="font-mono text-xs"
                        />
                      </td>
                      <td />
                    </tr>
                    {newKeyError && (
                      <tr className="bg-transparent!">
                        <td
                          colSpan={3}
                          className="px-3 pb-2 text-xs text-red-600 dark:text-red-400"
                        >
                          {newKeyError}
                        </td>
                      </tr>
                    )}
                  </>
                )}
              </tbody>
            </table>

            {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

            <footer className="flex items-center gap-2 p-3">
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  if (adding && newKey.trim() && !newKeyError) addRow();
                  setAdding(true);
                }}
                disabled={adding && (!newKey.trim() || !!newKeyError)}
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
      )}
    </CollapsibleSection>
  );
}
