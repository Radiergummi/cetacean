import { navigationShortcuts } from "../lib/shortcuts";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useEffect } from "react";

interface ShortcutGroup {
  title: string;
  shortcuts: { keys: string[]; description: string }[];
}

const groups: ShortcutGroup[] = [
  {
    title: "Global",
    shortcuts: [
      { keys: ["?"], description: "Show keyboard shortcuts" },
      { keys: ["/"], description: "Open search palette" },
      { keys: ["⌘", "K"], description: "Toggle search palette" },
      { keys: ["Esc"], description: "Close overlay / go back" },
    ],
  },
  {
    title: "Navigation",
    shortcuts: navigationShortcuts.map(({ keys, description }) => ({
      keys,
      description,
    })),
  },
  {
    title: "Lists",
    shortcuts: [
      { keys: ["j", "↓"], description: "Next row" },
      { keys: ["k", "↑"], description: "Previous row" },
      { keys: ["Enter"], description: "Open selected row" },
    ],
  },
];

/**
 * The `?` overlay.
 *
 * It rides on the shared Dialog rather than its own portal so that it traps
 * focus, returns it to whatever opened it, and dismisses on Escape and on an
 * outside press without hand-rolling any of it — the hand-rolled version had a
 * backdrop that only a mouse could dismiss and let Tab wander behind it.
 */
export default function ShortcutsHelp({ onClose }: { onClose: () => void }) {
  // `?` closes the overlay as well as opening it. Escape and outside presses
  // are the Dialog's own.
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "?") {
        event.preventDefault();
        onClose();
      }
    }

    document.addEventListener("keydown", onKeyDown);

    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) {
          onClose();
        }
      }}
    >
      <DialogContent
        showCloseButton={false}
        className="top-[10vh] max-w-lg translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-lg"
      >
        <DialogHeader className="flex-row items-center justify-between border-b px-4 py-3">
          <DialogTitle className="text-sm font-medium">Keyboard Shortcuts</DialogTitle>
          <kbd className="rounded border bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
            Esc
          </kbd>
        </DialogHeader>

        <div className="max-h-[60vh] space-y-5 overflow-y-auto p-4">
          {groups.map(({ shortcuts, title }) => (
            <div key={title}>
              <h3 className="mb-2 text-xs font-medium text-muted-foreground uppercase">{title}</h3>

              <div className="space-y-1.5">
                {shortcuts.map(({ description, keys }) => (
                  <div
                    key={description}
                    className="flex items-center justify-between text-sm"
                  >
                    <span className="text-muted-foreground">{description}</span>
                    <span className="flex items-center gap-1">
                      {keys.map((key) => (
                        <kbd
                          key={key}
                          className="inline-flex min-w-5 items-center justify-center rounded border bg-muted px-1.5 py-0.5 text-xs font-medium"
                        >
                          {key}
                        </kbd>
                      ))}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
