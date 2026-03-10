import { Search } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import SearchPalette from "./SearchPalette";

export default function GlobalSearch() {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setOpen((prev) => !prev);
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  const close = useCallback(() => setOpen(false), []);

  return (
    <>
      <button
        className="flex items-center gap-1.5 rounded-md border bg-muted/50 px-2.5 py-1 text-sm text-muted-foreground"
        onClick={() => setOpen(true)}
      >
        <Search className="w-3.5 h-3.5" />
        <span className="hidden sm:inline">Search...</span>
        <kbd className="hidden sm:inline-flex items-center rounded border bg-muted px-1 text-[10px] font-medium">
          ⌘K
        </kbd>
      </button>
      {open && <SearchPalette onClose={close} />}
    </>
  );
}
