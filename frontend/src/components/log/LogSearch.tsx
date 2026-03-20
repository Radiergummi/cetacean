import { Search, X } from "lucide-react";
import { type RefObject, useCallback, useEffect } from "react";

interface LogSearchProps {
  search: string;
  setSearch: (value: string) => void;
  caseSensitive: boolean;
  setCaseSensitive: (value: boolean) => void;
  useRegex: boolean;
  setUseRegex: (value: boolean) => void;
  matchIndex: number;
  setMatchIndex: (fn: (current: number) => number) => void;
  matchCount: number;
  searchRef: RefObject<HTMLInputElement | null>;
  logContainerRef: RefObject<HTMLElement | null>;
}

export function LogSearch({
  search,
  setSearch,
  caseSensitive,
  setCaseSensitive,
  useRegex,
  setUseRegex,
  matchIndex,
  setMatchIndex,
  matchCount,
  searchRef,
  logContainerRef,
}: LogSearchProps) {
  // Keyboard shortcut: Ctrl+F to focus search
  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      if (
        (event.metaKey || event.ctrlKey) &&
        event.key === "f" &&
        logContainerRef.current?.contains(document.activeElement)
      ) {
        event.preventDefault();
        searchRef.current?.focus();
      }
    },
    [logContainerRef, searchRef],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);

    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  return (
    <>
      <div className="relative flex items-center">
        <Search className="pointer-events-none absolute left-2 size-3.5 text-muted-foreground" />
        <input
          ref={searchRef}
          type="text"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder="Filter logs…"
          className="h-8 w-56 max-w-full rounded-md border bg-background pr-16 pl-7 font-mono text-xs"
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              setSearch("");
              return;
            }

            if (event.key === "Enter" && search && matchCount > 0) {
              event.preventDefault();

              if (event.shiftKey) {
                setMatchIndex((current) => (current - 1 + matchCount) % matchCount);
              } else {
                setMatchIndex((current) => (current + 1) % matchCount);
              }
            }
          }}
        />
        <div className="absolute right-1.5 flex items-center gap-0.5">
          <button
            onClick={() => setCaseSensitive(!caseSensitive)}
            aria-pressed={caseSensitive}
            className="rounded px-1 py-0.5 font-mono text-[10px] font-bold text-muted-foreground hover:text-foreground aria-pressed:bg-primary aria-pressed:text-primary-foreground"
            title="Case sensitive"
          >
            Aa
          </button>
          <button
            onClick={() => setUseRegex(!useRegex)}
            aria-pressed={useRegex}
            className="rounded px-1 py-0.5 font-mono text-[10px] font-bold text-muted-foreground hover:text-foreground aria-pressed:bg-primary aria-pressed:text-primary-foreground"
            title="Regex"
          >
            .*
          </button>
          {search && (
            <button
              onClick={() => setSearch("")}
              className="p-0.5 text-muted-foreground hover:text-foreground"
            >
              <X className="size-3" />
            </button>
          )}
        </div>
      </div>

      {search && (
        <span className="text-xs text-muted-foreground tabular-nums">
          {matchCount > 0 ? `${matchIndex + 1}/${matchCount}` : "0/0"}
        </span>
      )}
    </>
  );
}
