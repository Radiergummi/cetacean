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
        className="flex items-center justify-center xl:justify-normal gap-2 rounded-md xl:border hover:bg-muted
        xl:hover:bg-muted xl:bg-muted/50 size-8 xl:size-auto xl:ps-2 xl:pe-1.5 xl:py-1 text-sm xl:text-muted-foreground
        transition cursor-pointer"
        onClick={() => setOpen(true)}
      >
        <Search className="size-4 xl:size-3.5" />
        <span className="hidden xl:inline">Search…</span>
        <kbd className="hidden xl:inline-flex items-center rounded border bg-muted px-1 text-[10px] font-medium font-sans">
          {navigator.platform?.includes("Mac") ? "⌘" : "Ctrl"} K
        </kbd>
      </button>
      {open && <SearchPalette onClose={close} />}
    </>
  );
}
