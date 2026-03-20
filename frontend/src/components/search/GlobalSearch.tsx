import SearchPalette from "./SearchPalette";
import { Search } from "lucide-react";
import { forwardRef, useCallback, useEffect, useImperativeHandle, useState } from "react";

export interface GlobalSearchHandle {
  open: () => void;
}

const GlobalSearch = forwardRef<GlobalSearchHandle>(function GlobalSearch(_, ref) {
  const [open, setOpen] = useState(false);

  useImperativeHandle(ref, () => ({ open: () => setOpen(true) }), []);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key === "k") {
        event.preventDefault();
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
        className="flex size-8 cursor-pointer items-center justify-center gap-2 rounded-md text-sm transition select-none hover:bg-muted xl:size-auto xl:min-w-3xs xl:justify-between xl:border xl:bg-muted/50 xl:py-1 xl:ps-2 xl:pe-1.5 xl:text-muted-foreground xl:hover:bg-muted"
        onClick={() => setOpen(true)}
      >
        <Search className="size-4 xl:size-3.5" />
        <span className="hidden xl:inline">Search…</span>
        <kbd className="hidden items-center rounded border bg-muted px-1 font-sans text-[10px] font-medium xl:inline-flex">
          {navigator.platform?.includes("Mac") ? "⌘" : "Ctrl"} K
        </kbd>
      </button>
      {open && <SearchPalette onClose={close} />}
    </>
  );
});

export default GlobalSearch;
