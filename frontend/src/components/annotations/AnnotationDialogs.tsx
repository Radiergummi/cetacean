import { changedOnlyByRemovals } from "./annotationFields";
import AnnotationForm, { type EditedNote } from "./AnnotationForm";
import { api, ApiError } from "@/api/client";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { getErrorMessage } from "@/lib/utils";
import { useState } from "react";

/**
 * The form editing an annotation as it was read, open while `editing` is set.
 */
export function EditAnnotationDialog({
  editing,
  onClose,
}: {
  editing: EditedNote | null;
  onClose: () => void;
}) {
  return (
    <Dialog
      open={editing !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Edit annotation</DialogTitle>
          <DialogDescription>What it is about stays as it is.</DialogDescription>
        </DialogHeader>

        {editing && (
          <AnnotationForm
            editing={editing}
            onDone={onClose}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

/**
 * Asks before deleting the version of an annotation that was read, open while
 * `deleting` is set. A 412 caused only by a removal mark deletes again; any
 * other change asks once more against the version now current.
 */
export function DeleteAnnotationDialog({
  deleting,
  onClose,
  onDeleted,
}: {
  deleting: EditedNote | null;
  onClose: () => void;
  onDeleted?: (() => void) | undefined;
}) {
  const [target, setTarget] = useState(deleting);
  const [opened, setOpened] = useState(deleting);
  const [error, setError] = useState<string | null>(null);
  const [removing, setRemoving] = useState(false);

  if (opened !== deleting) {
    setOpened(deleting);
    setTarget(deleting);
    setError(null);
  }

  const done = () => {
    onClose();
    onDeleted?.();
  };

  const remove = async (note: EditedNote) => {
    setRemoving(true);
    setError(null);

    try {
      await api.deleteAnnotation(note.note.id, note.etag);
      done();
    } catch (failure) {
      if (failure instanceof ApiError && failure.status === 404) {
        // Someone else deleted it: what was asked for is done.
        done();
      } else if (failure instanceof ApiError && failure.status === 412) {
        try {
          const { data, etag } = await api.annotation(note.note.id);

          // A removal elsewhere changed the validator, not the annotation: delete again.
          if (changedOnlyByRemovals(note.note, data)) {
            try {
              await api.deleteAnnotation(data.id, etag);
              done();

              return;
            } catch (retry) {
              if (!(retry instanceof ApiError && retry.status === 412)) {
                setError(getErrorMessage(retry, "The annotation could not be deleted."));

                return;
              }
            }
          }

          setTarget({ note: data, etag });
          setError("This annotation changed since you opened it. Delete it anyway?");
        } catch (reason) {
          setError(getErrorMessage(reason, "The annotation could not be read."));
        }
      } else {
        setError(getErrorMessage(failure, "The annotation could not be deleted."));
      }
    } finally {
      setRemoving(false);
    }
  };

  return (
    <AlertDialog
      open={deleting !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete annotation?</AlertDialogTitle>
          <AlertDialogDescription>
            The annotation is removed for everyone. This can't be undone.
          </AlertDialogDescription>
        </AlertDialogHeader>

        {error && (
          <p
            role="alert"
            className="text-sm text-status-danger"
          >
            {error}
          </p>
        )}

        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <Button
            variant="destructive"
            disabled={removing}
            onClick={() => {
              if (target) {
                void remove(target);
              }
            }}
          >
            {removing ? "Deleting…" : "Delete"}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
