import { DeleteAnnotationDialog, EditAnnotationDialog } from "./AnnotationDialogs";
import type { EditedNote } from "./AnnotationForm";
import { api } from "@/api/client";
import { MenuContent, MenuItem } from "@/components/ui/menu";
import { getErrorMessage } from "@/lib/utils";
import { Menu } from "@base-ui/react/menu";
import { Ellipsis } from "lucide-react";
import { useEffect, useRef, useState } from "react";

type Loaded =
  | { status: "loading" }
  | { status: "failed"; message: string }
  | { status: "ready"; edited: EditedNote; methods: Set<string> };

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
          message: getErrorMessage(failure, "The annotation could not be read."),
        });
      }
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
          aria-label="Annotation actions"
          className="-my-0.5 inline-flex shrink-0 cursor-pointer items-center rounded-sm p-0.5 text-muted-foreground outline-none hover:bg-muted hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <Ellipsis className="size-4" />
        </Menu.Trigger>

        <MenuContent>
          {loaded.status === "ready" ? (
            <>
              {loaded.methods.has("PATCH") && (
                <MenuItem onClick={() => setEditing(loaded.edited)}>Edit</MenuItem>
              )}

              {loaded.methods.has("DELETE") && (
                <MenuItem
                  className="text-status-danger"
                  onClick={() => setDeleting(loaded.edited)}
                >
                  Delete
                </MenuItem>
              )}

              {!loaded.methods.has("PATCH") && !loaded.methods.has("DELETE") && (
                <MenuItem disabled>You can't change this annotation</MenuItem>
              )}
            </>
          ) : (
            <MenuItem disabled>
              {loaded.status === "loading" ? "Loading…" : loaded.message}
            </MenuItem>
          )}
        </MenuContent>
      </Menu.Root>

      <EditAnnotationDialog
        editing={editing}
        onClose={() => setEditing(null)}
      />

      <DeleteAnnotationDialog
        deleting={deleting}
        onClose={() => setDeleting(null)}
      />
    </>
  );
}
