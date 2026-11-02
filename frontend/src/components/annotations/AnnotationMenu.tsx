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
import { Menu } from "@base-ui/react/menu";
import { Ellipsis } from "lucide-react";
import { useEffect, useRef, useState } from "react";

type Loaded =
  | { status: "loading" }
  | { status: "failed"; message: string }
  | { status: "ready"; edited: EditedNote; methods: Set<string> };

const itemClass =
  "flex w-full cursor-default rounded-sm px-2 py-1.5 text-sm outline-none data-highlighted:bg-accent data-highlighted:text-accent-foreground data-disabled:text-muted-foreground";

/**
 * A note card's "…" menu. Opening it reads the note, whose `Allow` decides
 * what is offered and whose `ETag` every write from here sends back.
 * `onActiveChange` hears while the menu or one of its dialogs is open.
 */
export default function AnnotationMenu({
  id,
  onActiveChange,
}: {
  id: number;
  onActiveChange: (active: boolean) => void;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [loaded, setLoaded] = useState<Loaded>({ status: "loading" });
  const [editing, setEditing] = useState<EditedNote | null>(null);
  const [deleting, setDeleting] = useState<EditedNote | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [removing, setRemoving] = useState(false);
  const latest = useRef(0);
  const active = menuOpen || editing !== null || deleting !== null;

  useEffect(() => {
    if (!active) {
      return;
    }

    onActiveChange(true);

    return () => onActiveChange(false);
  }, [active, onActiveChange]);

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
        setLoaded({
          status: "failed",
          message: getErrorMessage(failure, "The note could not be read."),
        });
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
        try {
          const { data, etag } = await api.annotation(target.note.id);

          // Cancelled meanwhile, the confirmation stays closed.
          setDeleting((current) => current && { note: data, etag });
          setDeleteError("This note changed since you opened it. Delete it anyway?");
        } catch (reason) {
          setDeleteError(getErrorMessage(reason, "The note could not be read."));
        }
      } else {
        setDeleteError(getErrorMessage(failure, "The note could not be deleted."));
      }
    } finally {
      setRemoving(false);
    }
  };

  return (
    <>
      <Menu.Root
        modal={false}
        onOpenChange={(open) => {
          setMenuOpen(open);

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
              {loaded.status === "ready" ? (
                <>
                  {loaded.methods.has("PATCH") && (
                    <Menu.Item
                      className={itemClass}
                      onClick={() => setEditing(loaded.edited)}
                    >
                      Edit
                    </Menu.Item>
                  )}

                  {loaded.methods.has("DELETE") && (
                    <Menu.Item
                      className={`${itemClass} text-status-danger`}
                      onClick={() => {
                        setDeleteError(null);
                        setDeleting(loaded.edited);
                      }}
                    >
                      Delete
                    </Menu.Item>
                  )}

                  {!loaded.methods.has("PATCH") && !loaded.methods.has("DELETE") && (
                    <Menu.Item
                      disabled
                      className={itemClass}
                    >
                      You can't change this note
                    </Menu.Item>
                  )}
                </>
              ) : (
                <Menu.Item
                  disabled
                  className={itemClass}
                >
                  {loaded.status === "loading" ? "Loading…" : loaded.message}
                </Menu.Item>
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
