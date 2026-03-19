import { useEffect } from "react";
import { createPortal } from "react-dom";

interface ShortcutGroup {
  title: string;
  shortcuts: { keys: string[]; description: string }[];
}

const groups: ShortcutGroup[] = [
  {
    title: "Global",
    shortcuts: [
      { keys: ["?"], description: "Show keyboard shortcuts" },
      { keys: ["/"], description: "Focus search" },
      { keys: ["⌘", "K"], description: "Open search palette" },
      { keys: ["Esc"], description: "Close overlay / go back" },
    ],
  },
  {
    title: "Navigation",
    shortcuts: [
      { keys: ["g", "h"], description: "Go to cluster overview" },
      { keys: ["g", "n"], description: "Go to nodes" },
      { keys: ["g", "s"], description: "Go to services" },
      { keys: ["g", "a"], description: "Go to tasks" },
      { keys: ["g", "k"], description: "Go to stacks" },
      { keys: ["g", "c"], description: "Go to configs" },
      { keys: ["g", "x"], description: "Go to secrets" },
      { keys: ["g", "w"], description: "Go to networks" },
      { keys: ["g", "v"], description: "Go to volumes" },
      { keys: ["g", "i"], description: "Go to swarm info" },
      { keys: ["g", "t"], description: "Go to topology" },
    ],
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

export default function ShortcutsHelp({ onClose }: { onClose: () => void }) {
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape" || event.key === "?") {
        event.preventDefault();
        onClose();
      }
    }

    document.addEventListener("keydown", onKeyDown);

    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  return createPortal(
    <div
      className="fixed inset-0 z-50 animate-[fade-in_150ms_ease-out] bg-background/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="mx-auto mt-[10vh] max-w-lg animate-[slide-down_150ms_ease-out] overflow-hidden rounded-lg border bg-popover shadow-lg"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h2 className="text-sm font-medium">Keyboard Shortcuts</h2>
          <kbd className="rounded border bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
            Esc
          </kbd>
        </div>
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
                      {keys.map((key, index) => (
                        <span key={index}>
                          {index > 0 && <span className="mx-0.5 text-xs text-muted-foreground" />}
                          <kbd className="inline-flex min-w-5 items-center justify-center rounded border bg-muted px-1.5 py-0.5 text-xs font-medium">
                            {key}
                          </kbd>
                        </span>
                      ))}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>,
    document.body,
  );
}
