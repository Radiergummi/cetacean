import type { PatchOp } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import SimpleTable from "@/components/SimpleTable";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Pencil, Plus, Trash2, X } from "lucide-react";
import { useState } from "react";

interface KeyValueEditorProps {
  title: string;
  entries: Record<string, string>;
  keyLabel?: string;
  valueLabel?: string;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  onSave: (ops: PatchOp[]) => Promise<Record<string, string>>;
  defaultOpen?: boolean;
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
}: KeyValueEditorProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  function openEdit() {
    setDraft({ ...entries });
    setNewKey("");
    setNewValue("");
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

  async function save() {
    const ops: PatchOp[] = [];
    const effectiveDraft = newKey.trim() ? { ...draft, [newKey.trim()]: newValue } : draft;

    for (const key of Object.keys(entries)) {
      if (!(key in effectiveDraft)) {
        ops.push({ op: "remove", path: `/${key}` });
      }
    }
    for (const [key, value] of Object.entries(effectiveDraft)) {
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
      setSaveError(error instanceof Error ? error.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const sortedEntries = Object.entries(entries).sort(([a], [b]) => a.localeCompare(b));
  const draftEntries = Object.entries(draft).sort(([a], [b]) => a.localeCompare(b));

  const controls = !editing ? (
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
          <p className="text-sm text-muted-foreground">No {title.toLowerCase()}.</p>
        ) : (
          <SimpleTable
            columns={[keyLabel, valueLabel]}
            items={sortedEntries}
            keyFn={([key]) => key}
            renderRow={([key, value]) => (
              <>
                <td className="p-3 font-mono text-xs">{key}</td>
                <td className="p-3 font-mono text-xs break-all">{value}</td>
              </>
            )}
          />
        )
      ) : (
        <div className="space-y-3">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-max whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="p-3 text-left text-sm font-medium">{keyLabel}</th>
                  <th className="p-3 text-left text-sm font-medium">{valueLabel}</th>
                  <th className="p-3" />
                </tr>
              </thead>
              <tbody>
                {draftEntries.map(([key, value]) => (
                  <tr
                    key={key}
                    className="border-b last:border-b-0"
                  >
                    <td className="p-3 font-mono text-xs">{key}</td>
                    <td className="p-2">
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
                    </td>
                    <td className="p-2">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => removeRow(key)}
                        title="Remove"
                        className="text-muted-foreground hover:text-red-600"
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </td>
                  </tr>
                ))}
                <tr>
                  <td className="p-2">
                    <Input
                      value={newKey}
                      onChange={(event) => setNewKey(event.target.value)}
                      placeholder={keyPlaceholder}
                      className="font-mono text-xs"
                    />
                  </td>
                  <td className="p-2">
                    <Input
                      value={newValue}
                      onChange={(event) => setNewValue(event.target.value)}
                      placeholder={valuePlaceholder}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") addRow();
                      }}
                      className="font-mono text-xs"
                    />
                  </td>
                  <td className="p-2">
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      onClick={addRow}
                      disabled={!newKey.trim()}
                      title="Add"
                      className="text-muted-foreground hover:text-foreground"
                    >
                      <Plus className="size-3.5" />
                    </Button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}
          <div className="flex gap-2">
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
              <X className="size-3" />
              Cancel
            </Button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}
