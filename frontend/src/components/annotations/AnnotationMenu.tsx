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
import { Menu } from "@base-ui/react/menu";
import { Ellipsis } from "lucide-react";
import { useRef, useState } from "react";

type Loaded =
  | { status: "loading" }
  | { status: "failed"; message: string }
  | { status: "ready"; edited: EditedNote; methods: Set<string> };

const itemClass =
  "flex w-full cursor-default rounded-sm px-2 py-1.5 text-sm outline-none data-highlighted:bg-accent data-highlighted:text-accent-foreground";

function failureMessage(failure: unknown) {
  return failure instanceof ApiError ? failure.detail || failure.message : String(failure);
}

/**
 * A note card's "…" menu. Opening it reads the note, whose `Allow` decides
 * what is offered and whose `ETag` every write from here sends back.
 */
export default function AnnotationMenu({ id }: { id: number }) {
  const [loaded, setLoaded] = useState<Loaded>({ status: "loading" });
  const [editing, setEditing] = useState<EditedNote | null>(null);
  const [deleting, setDeleting] = useState<EditedNote | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [removing, setRemoving] = useState(false);
  const latest = useRef(0);

  const load = async () => {
    const request = ++latest.current;

    setLoaded({ status: "loading" });

    try {
      const { data, allowedMethods, etag } = await api.annotation(id);

      if (request === latest.current) {
        setLoaded({ status: "ready", edited: { note: data, etag }, methods: allowedMethods });
      }
    } catch (failure) {
      if (request === latest.current) {
        setLoaded({ status: "failed", message: failureMessage(failure) });
      }
    }
  };

  const remove = async (target: EditedNote) => {
    setRemoving(true);
    setDeleteError(null);

    try {
      await api.deleteAnnotation(target.note.id, target.etag);
      setDeleting(null);
    } catch (failure) {
      if (failure instanceof ApiError && failure.status === 412) {
        setDeleteError("This note changed since you opened it. Delete it anyway?");

        const { data, etag } = await api.annotation(target.note.id).catch(() => ({
          data: target.note,
          etag: undefined,
        }));

        setDeleting({ note: data, etag });
      } else {
        setDeleteError(failureMessage(failure));
      }
    } finally {
      setRemoving(false);
    }
  };

  const ready = loaded.status === "ready" ? loaded : null;
  const canEdit = ready?.methods.has("PATCH") ?? false;
  const canDelete = ready?.methods.has("DELETE") ?? false;

  return (
    <>
      <Menu.Root
        modal={false}
        onOpenChange={(open) => {
          if (open) {
            void load();
          }
        }}
      >
        <Menu.Trigger
          aria-label="Note actions"
          className="-my-0.5 inline-flex shrink-0 cursor-pointer items-center rounded-sm p-0.5 text-muted-foreground outline-none hover:bg-muted hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <Ellipsis className="size-4" />
        </Menu.Trigger>

        <Menu.Portal>
          <Menu.Positioner
            align="end"
            sideOffset={4}
            className="z-50"
          >
            <Menu.Popup className="min-w-36 rounded-md border bg-popover p-1 text-popover-foreground shadow-md outline-hidden">
              {loaded.status === "loading" && (
                <p className="px-2 py-1.5 text-sm text-muted-foreground">Loading…</p>
              )}

              {loaded.status === "failed" && (
                <p className="px-2 py-1.5 text-sm text-status-danger">{loaded.message}</p>
              )}

              {ready && canEdit && (
                <Menu.Item
                  className={itemClass}
                  onClick={() => setEditing(ready.edited)}
                >
                  Edit
                </Menu.Item>
              )}

              {ready && canDelete && (
                <Menu.Item
                  className={`${itemClass} text-status-danger`}
                  onClick={() => {
                    setDeleteError(null);
                    setDeleting(ready.edited);
                  }}
                >
                  Delete
                </Menu.Item>
              )}

              {ready && !canEdit && !canDelete && (
                <p className="px-2 py-1.5 text-sm text-muted-foreground">
                  You can't change this note
                </p>
              )}
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>

      <Dialog
        open={editing !== null}
        onOpenChange={(open) => {
          if (!open) {
            setEditing(null);
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
              onDone={() => setEditing(null)}
            />
          )}
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDeleting(null);
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete annotation?</AlertDialogTitle>
            <AlertDialogDescription>
              The note is removed for everyone. This can't be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>

          {deleteError && (
            <p
              role="alert"
              className="text-sm text-status-danger"
            >
              {deleteError}
            </p>
          )}

          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button
              variant="destructive"
              disabled={removing}
              onClick={() => {
                if (deleting) {
                  void remove(deleting);
                }
              }}
            >
              {removing ? "Deleting…" : "Delete"}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
