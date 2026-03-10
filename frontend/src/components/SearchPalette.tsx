import { Search } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { SearchResourceType, SearchResponse, SearchResult } from "../api/types";

const TYPE_ORDER: SearchResourceType[] = [
  "services", "stacks", "nodes", "tasks",
  "configs", "secrets", "networks", "volumes",
];

const TYPE_LABELS: Record<SearchResourceType, string> = {
  services: "Services", stacks: "Stacks", nodes: "Nodes", tasks: "Tasks",
  configs: "Configs", secrets: "Secrets", networks: "Networks", volumes: "Volumes",
};

function resourcePath(type: SearchResourceType, id: string): string {
  return `/${type}/${id}`;
}

interface FlatItem {
  type: SearchResourceType;
  result: SearchResult;
}

function flattenResults(response: SearchResponse): FlatItem[] {
  const items: FlatItem[] = [];
  for (const type of TYPE_ORDER) {
    const results = response.results[type];
    if (results) {
      for (const result of results) {
        items.push({ type, result });
      }
    }
  }
  return items;
}

export default function SearchPalette({ onClose }: { onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [response, setResponse] = useState<SearchResponse | null>(null);
  const [highlightIndex, setHighlightIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const navigate = useNavigate();

  const flat = response ? flattenResults(response) : [];

  const doSearch = useCallback((q: string) => {
    if (abortRef.current) abortRef.current.abort();
    if (!q.trim()) {
      setResponse(null);
      return;
    }
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    api.search(q).then((res) => {
      if (!ctrl.signal.aborted) {
        setResponse(res);
        setHighlightIndex(0);
      }
    }).catch(() => {
      // ignore aborted / failed requests
    });
  }, []);

  const onInputChange = useCallback((value: string) => {
    setQuery(value);
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => doSearch(value), 200);
  }, [doSearch]);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
      if (abortRef.current) abortRef.current.abort();
    };
  }, []);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const goTo = useCallback((item: FlatItem) => {
    navigate(resourcePath(item.type, item.result.id));
    onClose();
  }, [navigate, onClose]);

  const onKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setHighlightIndex((i) => Math.min(i + 1, flat.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setHighlightIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (flat[highlightIndex]) {
        goTo(flat[highlightIndex]);
      }
    } else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }, [flat, highlightIndex, goTo, onClose]);

  // Group items by type for rendering with section headers
  const groups: { type: SearchResourceType; items: { index: number; result: SearchResult }[] }[] = [];
  let idx = 0;
  for (const type of TYPE_ORDER) {
    const results = response?.results[type];
    if (results && results.length > 0) {
      const groupItems = results.map((r) => ({ index: idx++, result: r }));
      groups.push({ type, items: groupItems });
    }
  }

  const hasQuery = query.trim().length > 0;
  const hasResults = flat.length > 0;

  return (
    <div
      className="fixed inset-0 z-50 bg-background/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="mx-auto mt-[15vh] max-w-lg rounded-lg border bg-popover shadow-lg"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        {/* Search input */}
        <div className="flex items-center gap-2 border-b px-3 py-2.5">
          <Search className="w-4 h-4 text-muted-foreground shrink-0" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => onInputChange(e.target.value)}
            placeholder="Search resources..."
            className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
        </div>

        {/* Results area */}
        <div className="max-h-72 overflow-y-auto">
          {hasQuery && !hasResults && response && (
            <div className="px-3 py-6 text-center text-sm text-muted-foreground">
              No results for &ldquo;{query}&rdquo;
            </div>
          )}
          {groups.map((group) => (
            <div key={group.type}>
              <div className="px-3 py-1.5 text-xs font-medium uppercase text-muted-foreground">
                {TYPE_LABELS[group.type]}
              </div>
              {group.items.map((item) => (
                <button
                  key={`${group.type}-${item.result.id}`}
                  className={`flex w-full items-center gap-2 px-3 py-1.5 text-sm text-left ${
                    item.index === highlightIndex
                      ? "bg-accent text-accent-foreground"
                      : "text-foreground hover:bg-accent/50"
                  }`}
                  onClick={() => goTo({ type: group.type, result: item.result })}
                  onMouseEnter={() => setHighlightIndex(item.index)}
                >
                  <span className="truncate font-medium">{item.result.name}</span>
                  {item.result.detail && (
                    <span className="truncate text-muted-foreground text-xs">{item.result.detail}</span>
                  )}
                </button>
              ))}
            </div>
          ))}
        </div>

        {/* Footer */}
        {hasQuery && response && (
          <div className="flex items-center justify-between border-t px-3 py-2 text-xs text-muted-foreground">
            <span>{response.total} result{response.total !== 1 ? "s" : ""}</span>
            <button
              className="text-primary hover:underline"
              onClick={() => {
                navigate(`/search?q=${encodeURIComponent(query)}`);
                onClose();
              }}
            >
              View all results &rarr;
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
