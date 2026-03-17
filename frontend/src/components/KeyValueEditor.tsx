import CollapsibleSection from "./CollapsibleSection";
import SimpleTable from "./SimpleTable";
import { Spinner } from "./Spinner";
import { Pencil, Plus, Trash2, X } from "lucide-react";
import { useState } from "react";

interface PatchOp {
  op: string;
  path: string;
  value?: string;
}

interface KeyValueEditorProps {
  title: string;
  keyLabel: string;
  valueLabel: string;
  data: Record<string, string> | null;
  onSave: (ops: PatchOp[]) => Promise<void>;
  loading: boolean;
}

export default function KeyValueEditor({
  title,
  keyLabel,
  valueLabel,
  data,
  onSave,
  loading,
}: KeyValueEditorProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [newKey, setNewKey] = useState("");
  const [newVal, setNewVal] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  if (data === null || loading) return null;

  function openEdit() {
    setDraft({ ...data });
    setNewKey("");
    setNewVal("");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addRow() {
    const k = newKey.trim();
    if (!k) return;
    setDraft((prev) => ({ ...prev, [k]: newVal }));
    setNewKey("");
    setNewVal("");
  }

  function removeRow(key: string) {
    if (!window.confirm(`Remove ${keyLabel.toLowerCase()} "${key}"?`)) return;
    setDraft((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  }

  async function save() {
    const ops: PatchOp[] = [];
    const original = data!;

    for (const k of Object.keys(original)) {
      if (!(k in draft)) {
        ops.push({ op: "remove", path: `/${k}` });
      }
    }
    for (const [k, v] of Object.entries(draft)) {
      if (!(k in original)) {
        ops.push({ op: "add", path: `/${k}`, value: v });
      } else if (original[k] !== v) {
        ops.push({ op: "replace", path: `/${k}`, value: v });
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
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const entries = Object.entries(data).sort(([a], [b]) => a.localeCompare(b));
  const draftEntries = Object.entries(draft).sort(([a], [b]) => a.localeCompare(b));

  const controls = !editing ? (
    <button
      type="button"
      onClick={openEdit}
      className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs font-medium hover:bg-accent"
    >
      <Pencil className="h-3 w-3" />
      Edit
    </button>
  ) : null;

  return (
    <CollapsibleSection title={title} defaultOpen={false} controls={controls}>
      {!editing ? (
        entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">No {title.toLowerCase()}.</p>
        ) : (
          <SimpleTable
            columns={[keyLabel, valueLabel]}
            items={entries}
            keyFn={([k]) => k}
            renderRow={([k, v]) => (
              <>
                <td className="p-3 font-mono text-xs">{k}</td>
                <td className="p-3 font-mono text-xs break-all">{v}</td>
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
                {draftEntries.map(([k, v]) => (
                  <tr key={k} className="border-b last:border-b-0">
                    <td className="p-3 font-mono text-xs">{k}</td>
                    <td className="p-2">
                      <input
                        type="text"
                        value={v}
                        onChange={(e) => setDraft((prev) => ({ ...prev, [k]: e.target.value }))}
                        className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                      />
                    </td>
                    <td className="p-2">
                      <button
                        type="button"
                        onClick={() => removeRow(k)}
                        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-red-600"
                        title="Remove"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </td>
                  </tr>
                ))}
                <tr>
                  <td className="p-2">
                    <input
                      type="text"
                      value={newKey}
                      onChange={(e) => setNewKey(e.target.value)}
                      placeholder={keyLabel.toLowerCase()}
                      className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                    />
                  </td>
                  <td className="p-2">
                    <input
                      type="text"
                      value={newVal}
                      onChange={(e) => setNewVal(e.target.value)}
                      placeholder={valueLabel.toLowerCase()}
                      onKeyDown={(e) => { if (e.key === "Enter") addRow(); }}
                      className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                    />
                  </td>
                  <td className="p-2">
                    <button
                      type="button"
                      onClick={addRow}
                      disabled={!newKey.trim()}
                      className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-40"
                      title="Add"
                    >
                      <Plus className="h-3.5 w-3.5" />
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void save()}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground disabled:opacity-50"
            >
              {saving && <Spinner />}
              Save
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded border px-3 py-1.5 text-xs font-medium disabled:opacity-50"
            >
              <X className="h-3 w-3" />
              Cancel
            </button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}
