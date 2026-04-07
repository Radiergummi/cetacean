import { api } from "@/api/client";
import CollapsibleSection from "@/components/CollapsibleSection";
import { KeyValueEditor } from "@/components/KeyValueEditor";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { showErrorToast } from "@/lib/showErrorToast";
import { getErrorMessage } from "@/lib/utils";
import { Code, ExternalLink, Layers, Pencil } from "lucide-react";
import { type ReactNode, useState } from "react";

/**
 * Wrapper for integration panels that provides a toggle between
 * the structured view and raw label display, plus a docs link.
 * Supports inline editing in both structured and raw modes.
 *
 * A single `editing` flag controls both modes. Toggling between
 * structured and raw preserves the editing state.
 */
export function IntegrationSection({
  title,
  defaultOpen,
  enabled,
  rawLabels,
  docsUrl,
  children,
  editable,
  editContent,
  onEditStart,
  onSave,
  serviceId,
  onRawSave,
}: {
  title: string;
  defaultOpen: boolean;
  enabled: boolean;
  rawLabels: [string, string][];
  docsUrl: string;
  children: ReactNode;
  editable?: boolean;
  editContent?: ReactNode;
  onEditStart?: () => void;
  onSave?: () => Promise<void>;
  serviceId: string;
  onRawSave: (updated: Record<string, string>) => void;
}) {
  const [showRaw, setShowRaw] = useState(false);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  function cancel() {
    setEditing(false);
    setSaveError(null);
  }

  useEscapeCancel(editing && !showRaw, cancel);

  async function save() {
    if (!onSave) {
      return;
    }

    setSaving(true);
    setSaveError(null);

    try {
      await onSave();
      setEditing(false);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Save failed"));
      showErrorToast(error, "Save failed");
    } finally {
      setSaving(false);
    }
  }

  function startEditing() {
    onEditStart?.();
    setSaveError(null);
    setEditing(true);
  }

  return (
    <CollapsibleSection
      title={title}
      defaultOpen={defaultOpen}
      controls={
        <>
          <a
            href={docsUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
            onClick={(event) => event.stopPropagation()}
          >
            Docs
            <ExternalLink className="size-3" />
          </a>

          <Button
            variant="outline"
            size="xs"
            onClick={() => {
              setShowRaw((previous) => {
                if (editing && previous) {
                  onEditStart?.();
                }

                return !previous;
              });
            }}
          >
            {showRaw ? <Layers className="size-3" /> : <Code className="size-3" />}
            {showRaw ? "Structured" : "Labels"}
          </Button>

          {editable && !editing && (
            <Button
              variant="outline"
              size="xs"
              onClick={startEditing}
            >
              <Pencil className="size-3" />
              Edit
            </Button>
          )}
        </>
      }
    >
      {showRaw ? (
        <KeyValueEditor
          key={editing ? "editing" : "display"}
          title=""
          bare
          entries={Object.fromEntries(rawLabels)}
          defaultOpen
          editDisabled={!editing}
          defaultEditing={editing}
          onCancel={() => setEditing(false)}
          onSave={async (ops) => {
            const updated = await api.patchServiceLabels(serviceId, ops);
            onRawSave(updated);
            setEditing(false);
            return updated;
          }}
        />
      ) : editing ? (
        <div className="rounded-lg border p-3">
          <div className="space-y-4">
            {editContent}

            {saveError && <p className="text-xs text-destructive">{saveError}</p>}

            <footer className="flex items-center gap-2">
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
                  onClick={cancel}
                  disabled={saving}
                >
                  Cancel
                </Button>
              </div>
            </footer>
          </div>
        </div>
      ) : !enabled ? (
        <p className="text-sm text-muted-foreground">Disabled</p>
      ) : (
        children
      )}
    </CollapsibleSection>
  );
}
