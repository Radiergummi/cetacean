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
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape" || e.key === "?") {
        e.preventDefault();
        onClose();
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  return createPortal(
    <div
      className="fixed inset-0 z-50 bg-background/60 backdrop-blur-sm animate-[fade-in_150ms_ease-out]"
      onClick={onClose}
    >
      <div
        className="mx-auto mt-[10vh] max-w-lg rounded-lg border bg-popover shadow-lg animate-[slide-down_150ms_ease-out] overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h2 className="text-sm font-medium">Keyboard Shortcuts</h2>
          <kbd className="rounded border bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
            Esc
          </kbd>
        </div>
        <div className="max-h-[60vh] overflow-y-auto p-4 space-y-5">
          {groups.map((group) => (
            <div key={group.title}>
              <h3 className="text-xs font-medium uppercase text-muted-foreground mb-2">
                {group.title}
              </h3>
              <div className="space-y-1.5">
                {group.shortcuts.map((shortcut) => (
                  <div
                    key={shortcut.description}
                    className="flex items-center justify-between text-sm"
                  >
                    <span className="text-muted-foreground">{shortcut.description}</span>
                    <span className="flex items-center gap-1">
                      {shortcut.keys.map((key, i) => (
                        <span key={i}>
                          {i > 0 && <span className="text-muted-foreground text-xs mx-0.5" />}
                          <kbd className="inline-flex items-center justify-center min-w-5 rounded border bg-muted px-1.5 py-0.5 text-xs font-medium">
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
