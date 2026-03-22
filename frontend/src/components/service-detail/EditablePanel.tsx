import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import type { MouseEvent, ReactNode } from "react";
import { useCallback, useState } from "react";

interface EditablePanelProps {
  /** Read-only display content */
  display: ReactNode;
  /** Form content shown in edit mode */
  edit: ReactNode;
  /** Called when the user clicks Edit — use this to reset form state from current props */
  onOpen: () => void;
  /** Called when the user clicks Save — throw to show an error */
  onSave: () => Promise<void>;
}

export function EditablePanel({ display, edit, onOpen, onSave }: EditablePanelProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const cancelEdit = useCallback(() => {
    setEditing(false);
    setSaveError(null);
  }, []);

  useEscapeCancel(editing, cancelEdit);

  function openEdit(event: MouseEvent) {
    event.stopPropagation();
    onOpen();
    setSaveError(null);
    setEditing(true);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      await onSave();
      setEditing(false);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Save failed"));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-lg border p-3">
      {editing ? (
        <div className="space-y-4">
          {edit}

          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

          <footer className="flex items-center justify-end gap-2">
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
          </footer>
        </div>
      ) : (
        <div className="flex items-start justify-between gap-2">
          <div className="flex-1">{display}</div>

          {canEdit && (
            <Button
              variant="outline"
              size="xs"
              onClick={openEdit}
            >
              <Pencil className="size-3" />
              Edit
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
