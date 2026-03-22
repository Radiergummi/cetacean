import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import type { MouseEvent, ReactNode } from "react";
import { useState } from "react";

interface EditablePanelProps {
  /** Read-only display content */
  display: ReactNode;
  /** Form content shown in edit mode */
  edit: ReactNode;
  /** Called when the user clicks Edit — use this to reset form state from current props */
  onOpen: () => void;
  /** Called when the user clicks Save — throw to show an error */
  onSave: () => Promise<void>;
  /** Optional title shown above content in both modes */
  title?: string;
  /** Extra buttons rendered on the left side of the edit footer (e.g. "Add option") */
  actions?: ReactNode;
  /** When true, shows the empty state instead of display content */
  empty?: boolean;
  /** Description shown in the empty state when canEdit is true */
  emptyDescription?: string;
  /** Whether to wrap in a bordered div (default true) */
  bordered?: boolean;
}

export function EditablePanel({
  display,
  edit,
  onOpen,
  onSave,
  title,
  actions,
  empty,
  emptyDescription,
  bordered = true,
}: EditablePanelProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

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

  if (empty && !canEdit && !editing) {
    return null;
  }

  const titleRow = title && (
    <div className="flex items-center justify-between">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        {title}
      </h3>

      {!editing && canEdit && (
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
  );

  const wrapperClass = bordered ? "rounded-lg border p-3" : undefined;

  if (editing) {
    return (
      <div className={wrapperClass}>
        <div className="space-y-4">
          {titleRow}
          {edit}

          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

          <footer className="flex items-center gap-2">
            {actions}
            <div className={actions ? "ml-auto flex gap-2" : "ml-auto flex gap-2"}>
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
    );
  }

  if (empty) {
    return (
      <div className={wrapperClass}>
        <div className="space-y-3">
          {titleRow}
          <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
            <p className="text-sm">Not configured</p>
            {canEdit && emptyDescription && <p className="text-xs">{emptyDescription}</p>}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={wrapperClass}>
      <div className={title ? "space-y-3" : undefined}>
        {titleRow}
        <div className="flex items-start justify-between gap-2">
          <div className="flex-1">{display}</div>

          {!title && canEdit && (
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
      </div>
    </div>
  );
}
