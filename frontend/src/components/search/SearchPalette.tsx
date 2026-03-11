import { Search } from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";
import { api } from "../../api/client";
import type { SearchResourceType, SearchResponse, SearchResult } from "../../api/types";
import { resourcePath, statusColor, TYPE_LABELS, TYPE_ORDER } from "../../lib/searchConstants";
import { Spinner } from "../Spinner";

function StateOrb({ state }: { state: string }) {
  if (state === "updating") {
    return <Spinner className="size-3 shrink-0 text-blue-500" />;
  }
  const color = statusColor(state);
  return <span className={`inline-block size-2 rounded-full shrink-0 ${color}`} title={state} />;
}
import ResourceName from "../ResourceName";

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

  const doSearch = useCallback((query: string) => {
    if (abortRef.current) {
      abortRef.current.abort();
    }

    if (!query.trim()) {
      setResponse(null);
      return;
    }

    const controller = new AbortController();
    abortRef.current = controller;

    api
      .search(query)
      .then((response) => {
        if (!controller.signal.aborted) {
          setResponse(response);
          setHighlightIndex(0);
        }
      })
      .catch(() => {
        // ignore aborted / failed requests
      });
  }, []);

  const onInputChange = useCallback(
    (value: string) => {
      setQuery(value);

      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }

      timerRef.current = setTimeout(() => doSearch(value), 200);
    },
    [doSearch],
  );

  useEffect(() => {
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }

      if (abortRef.current) {
        abortRef.current.abort();
      }
    };
  }, []);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Poll every 2s to refresh state/detail on existing results (no reorder/add/remove)
  useEffect(() => {
    if (!response || !query.trim()) return;
    const interval = setInterval(() => {
      api
        .search(query)
        .then((fresh) => {
          setResponse((prev) => {
            if (!prev) return prev;
            const updated = { ...prev, total: fresh.total, counts: fresh.counts };
            const newResults: typeof prev.results = {};
            for (const type of TYPE_ORDER) {
              const prevItems = prev.results[type];
              if (!prevItems) continue;
              const freshItems = fresh.results[type];
              // Build lookup from fresh data
              const freshMap = new Map<string, { detail: string; state?: string }>();
              if (freshItems) {
                for (const item of freshItems) {
                  freshMap.set(item.id, { detail: item.detail, state: item.state });
                }
              }
              // Update in-place: same order, same items, just refresh mutable fields
              newResults[type] = prevItems.map((item) => {
                const freshData = freshMap.get(item.id);
                if (freshData) {
                  return { ...item, detail: freshData.detail, state: freshData.state };
                }
                return item;
              });
            }
            updated.results = newResults;
            return updated;
          });
        })
        .catch(() => {
          /* ignore */
        });
    }, 2000);
    return () => clearInterval(interval);
  }, [query, response]);

  const goTo = useCallback(
    ({ result: { id }, type }: FlatItem) => {
      navigate(resourcePath(type, id));
      onClose();
    },
    [navigate, onClose],
  );

  const onKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setHighlightIndex((i) => Math.min(i + 1, flat.length - 1));
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        setHighlightIndex((i) => Math.max(i - 1, 0));
      } else if (event.key === "Enter") {
        event.preventDefault();
        if (flat[highlightIndex]) {
          goTo(flat[highlightIndex]);
        }
      } else if (event.key === "Escape") {
        event.preventDefault();
        onClose();
      }
    },
    [flat, highlightIndex, goTo, onClose],
  );

  // Group items by type for rendering with section headers
  const groups: { type: SearchResourceType; items: { index: number; result: SearchResult }[] }[] =
    [];
  let idx = 0;

  for (const type of TYPE_ORDER) {
    const results = response?.results[type];

    if (results && results.length > 0) {
      const items = results.map((result) => ({ index: idx++, result }));

      groups.push({ type, items });
    }
  }

  const hasQuery = query.trim().length > 0;
  const hasResults = flat.length > 0;

  return createPortal(
    <div
      className="fixed inset-0 z-50 bg-background/60 backdrop-blur-sm animate-[fade-in_150ms_ease-out]"
      onClick={onClose}
    >
      <div
        className="mx-auto mt-[15vh] max-w-lg rounded-lg border bg-popover shadow-lg animate-[slide-down_150ms_ease-out]"
        onClick={(event) => event.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        {/* Search input */}
        <div className="flex items-center gap-2 border-b px-3 py-2.5">
          <Search className="size-4 text-muted-foreground shrink-0" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(event) => onInputChange(event.target.value)}
            placeholder="Search resources…"
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

          <ul className="flex flex-col gap-3">
            {groups.map(({ items, type }) => (
              <li key={type}>
                <section>
                  <header className="px-3 py-1.5 text-xs font-medium uppercase text-muted-foreground">
                    <span>{TYPE_LABELS[type]}</span>
                  </header>

                  {items.map(({ index, result }) => (
                    <button
                      key={`${type}-${result.id}`}
                      data-active={index === highlightIndex || undefined}
                      className="flex w-full justify-between items-center gap-2 px-3 py-1.5 text-sm text-left cursor-pointer text-foreground hover:bg-accent/50 data-active:bg-accent data-active:text-accent-foreground"
                      onClick={() => goTo({ type, result })}
                      onMouseEnter={() => setHighlightIndex(index)}
                    >
                      <span className="truncate font-medium flex items-center gap-1.5">
                        {result.state && <StateOrb state={result.state} />}
                        <ResourceName name={result.name} />
                      </span>
                      {result.detail && (
                        <span className="truncate text-muted-foreground text-xs">
                          {result.detail}
                        </span>
                      )}
                    </button>
                  ))}
                </section>
              </li>
            ))}
          </ul>
        </div>

        {/* Footer */}
        {hasQuery && response && (
          <div className="flex items-center justify-between border-t px-3 py-2 text-xs text-muted-foreground">
            <span>
              {response.total} result{response.total !== 1 ? "s" : ""}
            </span>
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
    </div>,
    document.body,
  );
}
