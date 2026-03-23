import CollapsibleSection from "@/components/CollapsibleSection";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil, Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { useState } from "react";

interface EditableTableProps<T> {
  title: string;
  titleExtra?: ReactNode;
  items: T[];
  columns: [string, string];
  defaultOpen?: boolean;
  editDisabled?: boolean;

  // Read-only view
  renderReadOnly: (items: T[]) => ReactNode;
  emptyLabel: string;
  emptyHint?: string;

  // Edit mode — existing rows
  keyFn: (item: T, index: number) => string | number;
  renderKeyCell: (item: T, index: number) => ReactNode;
  renderValueCell: (item: T, index: number, update: (next: T) => void) => ReactNode;
  /** Return false to hide the remove button for a specific row. Defaults to true. */
  canRemove?: (item: T, index: number) => boolean;

  // Edit mode — add row
  renderAddKeyCell: (draft: T[]) => ReactNode;
  renderAddValueCell: (draft: T[]) => ReactNode;
  renderAddError?: () => ReactNode;
  canAdd: boolean;
  onAddCommit: () => T | null;
  onAddReset: () => void;

  // Save
  onSave: (items: T[]) => Promise<void>;
}

export function EditableTable<T>({
  title,
  titleExtra,
  items,
  columns,
  defaultOpen = false,
  editDisabled = false,
  renderReadOnly,
  emptyLabel,
  emptyHint,
  keyFn,
  renderKeyCell,
  renderValueCell,
  canRemove,
  renderAddKeyCell,
  renderAddValueCell,
  renderAddError,
  canAdd,
  onAddCommit,
  onAddReset,
  onSave,
}: EditableTableProps<T>) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [draft, setDraft] = useState<T[]>([]);
  const [adding, setAdding] = useState(false);

  useEscapeCancel(editing, () => cancelEdit());

  function openEdit() {
    setDraft([...items]);
    setAdding(items.length === 0);
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function removeRow(index: number) {
    setDraft((previous) => previous.filter((_, i) => i !== index));
  }

  function handleAddAnother() {
    if (adding && canAdd) {
      const result = onAddCommit();

      if (result === null) {
        return;
      }

      setDraft((previous) => [...previous, result]);
      onAddReset();
    }

    setAdding(true);
  }

  async function save() {
    let effectiveDraft = draft;

    if (adding && canAdd) {
      const result = onAddCommit();

      if (result !== null) {
        effectiveDraft = [...draft, result];
      }
    }

    setSaving(true);
    setSaveError(null);

    try {
      await onSave(effectiveDraft);
      setEditing(false);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Save failed"));
    } finally {
      setSaving(false);
    }
  }

  const hasControls = (editing && titleExtra) || (!editing && !editDisabled);
  const controls = hasControls ? (
    <>
      {editing && titleExtra}
      {!editing && !editDisabled && (
        <Button
          variant="outline"
          size="xs"
          onClick={openEdit}
        >
          <Pencil className="size-3" />
          Edit
        </Button>
      )}
    </>
  ) : null;

  const addError = renderAddError?.();

  return (
    <CollapsibleSection
      title={title}
      defaultOpen={defaultOpen}
      controls={controls}
    >
      {!editing ? (
        items.length === 0 ? (
          <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
            <p className="text-sm">{emptyLabel}</p>
            {!editDisabled && emptyHint && <p className="text-xs">{emptyHint}</p>}
          </div>
        ) : (
          renderReadOnly(items)
        )
      ) : (
        <div className="flex flex-col gap-3">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-max whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-background/50">
                <tr className="bg-muted/50 dark:bg-transparent">
                  <th className="ps-3 pt-3 pb-1.5 text-left text-sm font-medium">{columns[0]}</th>
                  <th className="ps-3 pt-3 pb-1.5 text-left text-sm font-medium">{columns[1]}</th>
                  <th className="w-12 py-3 ps-3 pb-1.5" />
                </tr>
              </thead>
              <tbody>
                {draft.map((item, index) => (
                  <tr
                    key={keyFn(item, index)}
                    className="border-b bg-transparent! last:border-b-0"
                  >
                    <td className="py-3 ps-3">{renderKeyCell(item, index)}</td>
                    <td className="py-3 ps-3">
                      {renderValueCell(item, index, (next) =>
                        setDraft((previous) =>
                          previous.map((existing, i) => (i === index ? next : existing)),
                        ),
                      )}
                    </td>
                    <td className="py-3 ps-3">
                      {(canRemove?.(item, index) ?? true) && (
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          onClick={() => removeRow(index)}
                          title="Remove"
                          className="text-muted-foreground hover:text-red-600"
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
                {adding && (
                  <>
                    <tr className="bg-transparent!">
                      <td className="py-3 ps-3">{renderAddKeyCell(draft)}</td>
                      <td className="py-3 ps-3">{renderAddValueCell(draft)}</td>
                      <td />
                    </tr>
                    {addError && (
                      <tr className="bg-transparent!">
                        <td
                          colSpan={3}
                          className="px-3 pb-2 text-xs text-red-600 dark:text-red-400"
                        >
                          {addError}
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
                onClick={handleAddAnother}
                disabled={adding && !canAdd}
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
