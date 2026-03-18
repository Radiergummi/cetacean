import {Pencil, Plus, Trash2, X} from "lucide-react";
import {useState} from "react";
import {api} from "../../api/client";
import CollapsibleSection from "../CollapsibleSection";
import SimpleTable from "../SimpleTable";
import {Spinner} from "../Spinner";

export function EnvEditor({
  serviceId,
  envVars,
  onSaved,
}: {
  serviceId: string;
  envVars: Record<string, string>;
  onSaved: (updated: Record<string, string>) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [newKey, setNewKey] = useState("");
  const [newVal, setNewVal] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  function openEdit() {
    setDraft({...envVars});
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
    const key = newKey.trim();
    if (!key) {
      return;
    }

    setDraft((prev) => ({...prev, [key]: newVal}));
    setNewKey("");
    setNewVal("");
  }

  function removeRow(key: string) {
    if (!window.confirm(`Remove env var "${key}"?`)) {
      return;
    }

    setDraft((prev) => {
      const next = {...prev};
      delete next[key];
      return next;
    });
  }

  async function save() {
    const ops: Array<{ op: string; path: string; value?: string }> = [];
    const original = envVars;

    const effectiveDraft =
      newKey.trim() ? {...draft, [newKey.trim()]: newVal} : draft;

    for (const key of Object.keys(original)) {
      if (!(key in effectiveDraft)) {
        ops.push({op: "remove", path: `/${key}`});
      }
    }

    for (const [key, value] of Object.entries(effectiveDraft)) {
      if (!(key in original)) {
        ops.push({op: "add", path: `/${key}`, value: value});
      } else if (original[key] !== value) {
        ops.push({op: "replace", path: `/${key}`, value: value});
      }
    }

    if (ops.length === 0) {
      setEditing(false);
      return;
    }

    setSaving(true);
    setSaveError(null);

    try {
      const updated = await api.patchServiceEnv(serviceId, ops);
      onSaved(updated);
      setEditing(false);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const entries = Object
    .entries(envVars)
    .sort(([a], [b]) => a.localeCompare(b));
  const draftEntries = Object
    .entries(draft)
    .sort(([a], [b]) => a.localeCompare(b));

  const controls = !editing ? (
    <button
      type="button"
      onClick={openEdit}
      className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs font-medium hover:bg-accent"
    >
      <Pencil className="size-3"/>
      Edit
    </button>
  ) : null;

  return (
    <CollapsibleSection
      title="Environment Variables"
      defaultOpen={false}
      controls={controls}
    >
      {!editing ? (
        entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">No environment variables.</p>
        ) : (
          <SimpleTable
            columns={["Variable", "Value"]}
            items={entries}
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
                <th className="p-3 text-left text-sm font-medium">Variable</th>
                <th className="p-3 text-left text-sm font-medium">Value</th>
                <th className="p-3"/>
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
                      onChange={(e) => setDraft((prev) => ({...prev, [k]: e.target.value}))}
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
                      <Trash2 className="h-3.5 w-3.5"/>
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
                    placeholder="NEW_VAR"
                    className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                  />
                </td>
                <td className="p-2">
                  <input
                    type="text"
                    value={newVal}
                    onChange={(e) => setNewVal(e.target.value)}
                    placeholder="value"
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        addRow();
                      }
                    }}
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
                    <Plus className="h-3.5 w-3.5"/>
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
              {saving && <Spinner className="size-3"/>}
              Save
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded border px-3 py-1.5 text-xs font-medium disabled:opacity-50"
            >
              <X className="size-3"/>
              Cancel
            </button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}
