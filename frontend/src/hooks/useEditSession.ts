import { useEscapeCancel } from "./useEscapeCancel";
import { useLeaveGuard } from "./useLeaveGuard";
import { showErrorToast } from "@/lib/showErrorToast";
import { getErrorMessage } from "@/lib/utils";
import { useState } from "react";

interface EditSessionOptions {
  /** Seeds the form from the current value; called as editing starts. */
  onOpen?: (() => void) | undefined;
  /** Throw to keep the form open and show the error. */
  onSave: () => Promise<void>;
  onCancel?: (() => void) | undefined;
  /** Shown when the thrown error carries no message. */
  errorMessage?: string | undefined;
  /** Also raise a toast when saving fails. */
  toast?: boolean | undefined;
  initialEditing?: boolean | undefined;
  /** Whether Escape cancels while editing (default true). */
  escapeCancels?: boolean | undefined;
}

/**
 * The open → edit → save or cancel lifecycle of an inline editor, with the
 * Escape and leave-page guards every one of them installs while editing.
 */
export function useEditSession({
  onOpen,
  onSave,
  onCancel,
  errorMessage = "Save failed",
  toast = false,
  initialEditing = false,
  escapeCancels = true,
}: EditSessionOptions) {
  const [editing, setEditing] = useState(initialEditing);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  function open() {
    onOpen?.();
    setSaveError(null);
    setEditing(true);
  }

  function cancel() {
    setEditing(false);
    setSaveError(null);
    onCancel?.();
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      await onSave();
      setEditing(false);
    } catch (error) {
      setSaveError(getErrorMessage(error, errorMessage));

      if (toast) {
        showErrorToast(error, errorMessage);
      }
    } finally {
      setSaving(false);
    }
  }

  useEscapeCancel(editing && escapeCancels, cancel);
  useLeaveGuard(editing);

  return { editing, saving, saveError, open, cancel, save };
}
