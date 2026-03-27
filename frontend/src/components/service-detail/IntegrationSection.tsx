import { api } from "@/api/client";
import CollapsibleSection from "@/components/CollapsibleSection";
import { KeyValueEditor } from "@/components/KeyValueEditor";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { showErrorToast } from "@/lib/showErrorToast";
import { Code, ExternalLink, Layers, Pencil } from "lucide-react";
import { useState } from "react";

/**
 * Wrapper for integration panels that provides a toggle between
 * the structured view and raw label display, plus a docs link.
 * Supports inline editing in both structured and raw modes.
 *
 * A single `editing` flag controls both modes. Toggling between
 * structured and raw preserves the editing state. In raw edit mode,
 * the KeyValueEditor is mounted with `defaultEditing=true` and
 * `editDisabled` so IntegrationSection's own Save/Cancel are not
 * needed — KVEditor handles its own save. On KVEditor save/cancel,
 * it resets `editing` via the callbacks.
 */
export function IntegrationSection({
  title,
  defaultOpen,
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
  rawLabels: [string, string][];
  docsUrl: string;
  children: React.ReactNode;
  editable?: boolean;
  editContent?: React.ReactNode;
  onEditStart?: () => void;
  onSave?: () => Promise<void>;
  serviceId?: string;
  onRawSave?: (updated: Record<string, string>) => void;
}) {
  const [showRaw, setShowRaw] = useState(false);
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);

  function cancel() {
    setEditing(false);
  }

  useEscapeCancel(editing && !showRaw, cancel);

  async function save() {
    if (!onSave) {
      return;
    }

    setSaving(true);

    try {
      await onSave();
      setEditing(false);
    } catch (error) {
      showErrorToast(error, "Save failed");
    } finally {
      setSaving(false);
    }
  }

  function startEditing() {
    onEditStart?.();
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
            <Button variant="outline" size="xs" onClick={startEditing}>
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
            const updated = await api.patchServiceLabels(serviceId!, ops);
            onRawSave?.(updated);
            setEditing(false);
            return updated;
          }}
        />
      ) : editing ? (
        <div className="rounded-lg border p-3">
          <div className="space-y-4">
            {editContent}

            <footer className="flex items-center gap-2">
              <div className="ml-auto flex gap-2">
                <Button size="sm" onClick={save} disabled={saving}>
                  {saving && <Spinner className="size-3" />}
                  Save
                </Button>
                <Button variant="outline" size="sm" onClick={cancel} disabled={saving}>
                  Cancel
                </Button>
              </div>
            </footer>
          </div>
        </div>
      ) : (
        children
      )}
    </CollapsibleSection>
  );
}
