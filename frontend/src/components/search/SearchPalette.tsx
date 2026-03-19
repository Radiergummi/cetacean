import { api } from "../../api/client";
import type { SearchResourceType, SearchResponse, SearchResult } from "../../api/types";
import { getActions, matchAction, type PaletteAction, type PaletteStep } from "../../lib/actions";
import { resourcePath, statusColor, typeLabels, typeOrder } from "../../lib/searchConstants";
import { getErrorMessage } from "../../lib/utils";
import ResourceName from "../ResourceName";
import { Spinner } from "../Spinner";
import { ChevronRight, Search, Zap } from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";

function StateOrb({ state }: { state: string }) {
  if (state === "updating") {
    return <Spinner className="size-3 shrink-0 text-blue-500" />;
  }

  const color = statusColor(state);

  return (
    <span
      className={`inline-block size-2 shrink-0 rounded-full ${color}`}
      title={state}
    />
  );
}

interface FlatItem {
  type: SearchResourceType;
  result: SearchResult;
}

function flattenResults(response: SearchResponse, filterType?: SearchResourceType): FlatItem[] {
  const items: FlatItem[] = [];
  const types = filterType ? [filterType] : typeOrder;

  for (const type of types) {
    const results = response.results[type];

    if (results) {
      for (const result of results) {
        items.push({ type, result });
      }
    }
  }

  return items;
}

/** Map singular resource type from action steps to plural SearchResourceType */
function toSearchType(resourceType: string): SearchResourceType {
  return (resourceType + "s") as SearchResourceType;
}

function ActionBreadcrumbs({
  action,
  steps,
  actionArgs,
  currentStep,
}: {
  action: PaletteAction;
  steps: PaletteStep[];
  actionArgs: unknown[];
  currentStep: number;
}) {
  return (
    <div className="flex items-center gap-1 border-b px-3 py-1.5 text-xs text-muted-foreground">
      <span className="font-medium text-foreground">{action.label}</span>
      {steps.map(({ label, type }, index) => (
        <span
          key={label}
          className="flex items-center gap-1"
        >
          <ChevronRight className="size-3" />
          {index < currentStep ? (
            <span className="text-foreground">
              {type === "resource"
                ? ((actionArgs[index] as { name?: string })?.name ?? String(actionArgs[index]))
                : String(actionArgs[index])}
            </span>
          ) : index === currentStep ? (
            <span className="font-medium text-primary">{label}</span>
          ) : (
            <span>{label}</span>
          )}
        </span>
      ))}
    </div>
  );
}

export default function SearchPalette({ onClose }: { onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [response, setResponse] = useState<SearchResponse | null>(null);
  const [highlightIndex, setHighlightIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const navigate = useNavigate();

  // Action mode state
  const actions = useMemo(() => getActions(), []);
  const [activeAction, setActiveAction] = useState<PaletteAction | null>(null);
  const [actionStep, setActionStep] = useState(0);
  const [actionArgs, setActionArgs] = useState<unknown[]>([]);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionLoading, setActionLoading] = useState(false);

  // Detect action match when not yet in action mode
  const actionMatch = useMemo(() => {
    if (activeAction) {
      return null;
    }

    return matchAction(query, actions);
  }, [query, actions, activeAction]);

  const currentStep = activeAction ? activeAction.steps[actionStep] : null;

  // Determine the resource type filter when in action resource-picker mode
  const resourceFilter: SearchResourceType | undefined =
    currentStep?.type === "resource" && currentStep.resourceType
      ? toSearchType(currentStep.resourceType)
      : undefined;

  const flat = useMemo(
    () => (response ? flattenResults(response, resourceFilter) : []),
    [response, resourceFilter],
  );
  const hasResponse = response !== null;

  // Total items: action suggestion (if any) + flat results
  const actionSuggestionOffset = actionMatch && !activeAction ? 1 : 0;
  const totalItems = flat.length + actionSuggestionOffset;

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
      .search(query, undefined, controller.signal)
      .then((r) => {
        if (!controller.signal.aborted) {
          setResponse(r);
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
      setActionError(null);

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
    if (!hasResponse || !query.trim()) {
      return;
    }
    const controller = new AbortController();
    const interval = setInterval(() => {
      api
        .search(query, undefined, controller.signal)
        .then((fresh) => {
          if (controller.signal.aborted) {
            return;
          }

          setResponse((previous) => {
            if (!previous) {
              return previous;
            }

            const updated = { ...previous, total: fresh.total, counts: fresh.counts };
            const newResults: typeof previous.results = {};

            for (const type of typeOrder) {
              const prevItems = previous.results[type];

              if (!prevItems) {
                continue;
              }

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
    }, 2_000);
    return () => {
      controller.abort();
      clearInterval(interval);
    };
  }, [query, hasResponse]);

  const goTo = useCallback(
    ({ result: { id }, type }: FlatItem) => {
      navigate(resourcePath(type, id)!);
      onClose();
    },
    [navigate, onClose],
  );

  const activateAction = useCallback((action: PaletteAction) => {
    setActiveAction(action);
    setActionStep(0);
    setActionArgs([]);
    setActionError(null);
    setQuery("");
    setResponse(null);
    setHighlightIndex(0);
  }, []);

  const executeAction = useCallback(
    async (action: PaletteAction, args: unknown[]) => {
      if (action.destructive) {
        const confirmed = window.confirm(`Confirm: ${action.label}?`);

        if (!confirmed) {
          return;
        }
      }

      setActionLoading(true);
      setActionError(null);

      try {
        await action.execute(...args);

        onClose();
      } catch (err) {
        setActionError(getErrorMessage(err, String(err)));
      } finally {
        setActionLoading(false);
      }
    },
    [onClose],
  );

  const advanceStep = useCallback(
    (value: unknown) => {
      if (!activeAction) {
        return;
      }

      const newArgs = [...actionArgs, value];

      setActionArgs(newArgs);

      if (actionStep + 1 >= activeAction.steps.length) {
        // All steps done, execute
        void executeAction(activeAction, newArgs);
      } else {
        setActionStep(actionStep + 1);
        setQuery("");
        setResponse(null);
        setHighlightIndex(0);
      }
    },
    [activeAction, actionArgs, actionStep, executeAction],
  );

  const goBackStep = useCallback(() => {
    if (!activeAction) {
      return;
    }

    if (actionStep === 0) {
      // Exit action mode
      setActiveAction(null);
      setActionArgs([]);
      setActionStep(0);
      setQuery("");
      setResponse(null);
      setHighlightIndex(0);
    } else {
      setActionStep(actionStep - 1);
      setActionArgs(actionArgs.slice(0, -1));
      setQuery("");
      setResponse(null);
      setHighlightIndex(0);
    }
    setActionError(null);
  }, [activeAction, actionStep, actionArgs]);

  const selectItem = useCallback(
    (item: FlatItem) => {
      if (activeAction && currentStep?.type === "resource") {
        // Pass the search result as the arg (has id and name)
        advanceStep(item.result);
      } else {
        goTo(item);
      }
    },
    [activeAction, currentStep, advanceStep, goTo],
  );

  const onKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setHighlightIndex((i) => Math.min(i + 1, totalItems - 1));
      } else if (event.key === "ArrowUp") {
        event.preventDefault();
        setHighlightIndex((i) => Math.max(i - 1, 0));
      } else if (event.key === "Enter") {
        event.preventDefault();

        // In action mode with number/text step, submit the input value
        if (
          activeAction &&
          currentStep &&
          (currentStep.type === "number" || currentStep.type === "text")
        ) {
          const val = currentStep.type === "number" ? Number(query) : query;

          if (currentStep.type === "number" && (isNaN(val as number) || query.trim() === "")) {
            return;
          }

          if (currentStep.type === "text" && query.trim() === "") {
            return;
          }

          advanceStep(val);

          return;
        }

        // Action suggestion at index 0
        if (actionMatch && highlightIndex === 0) {
          activateAction(actionMatch.action);
          return;
        }

        // Regular result
        const flatIndex = highlightIndex - actionSuggestionOffset;

        if (flat[flatIndex]) {
          selectItem(flat[flatIndex]);
        }
      } else if (event.key === "Escape") {
        event.preventDefault();

        if (activeAction) {
          goBackStep();
        } else {
          onClose();
        }
      } else if (event.key === "Backspace" && query === "" && activeAction) {
        event.preventDefault();
        goBackStep();
      }
    },
    [
      totalItems,
      activeAction,
      currentStep,
      actionMatch,
      highlightIndex,
      actionSuggestionOffset,
      flat,
      query,
      advanceStep,
      activateAction,
      selectItem,
      goBackStep,
      onClose,
    ],
  );

  // Compute placeholder
  const placeholder =
    activeAction && currentStep
      ? currentStep.type === "resource"
        ? `Search for a ${currentStep.label}…`
        : (currentStep.placeholder ?? `Enter ${currentStep.label.toLowerCase()}…`)
      : "Search resources…";

  // Group items by type for rendering with section headers
  const groups: { type: SearchResourceType; items: { index: number; result: SearchResult }[] }[] =
    [];
  let idx = actionSuggestionOffset;

  const typesToRender = resourceFilter ? [resourceFilter] : typeOrder;

  for (const type of typesToRender) {
    const results = response?.results[type];

    if (results && results.length > 0) {
      const items = results.map((result) => ({ index: idx++, result }));

      groups.push({ type, items });
    }
  }

  const hasQuery = query.trim().length > 0;
  const hasResults = flat.length > 0;
  const showSearchResults = currentStep?.type === "resource" || !activeAction;

  return createPortal(
    <div
      className="fixed inset-0 z-50 animate-[fade-in_150ms_ease-out] bg-background/60 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="mx-4 mt-[5vh] max-w-lg animate-[slide-down_150ms_ease-out] rounded-lg border bg-popover shadow-lg md:mx-auto md:mt-[15vh]"
        onClick={(event) => event.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        {activeAction && (
          <ActionBreadcrumbs
            action={activeAction}
            steps={activeAction.steps}
            actionArgs={actionArgs}
            currentStep={actionStep}
          />
        )}

        <div className="flex items-center gap-2 border-b px-3 py-2.5">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <input
            ref={inputRef}
            type={currentStep?.type === "number" ? "number" : "text"}
            value={query}
            onChange={(event) => onInputChange(event.target.value)}
            placeholder={placeholder}
            className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
          {actionLoading && <Spinner className="size-4 shrink-0" />}
        </div>

        {actionError && (
          <div className="border-b bg-destructive/10 px-3 py-2 text-xs text-destructive">
            {actionError}
          </div>
        )}

        <div className="max-h-72 overflow-y-auto">
          {actionMatch && !activeAction && (
            <button
              data-active={highlightIndex === 0 || undefined}
              className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left text-sm font-medium text-foreground hover:bg-accent/50 data-active:bg-accent data-active:text-accent-foreground"
              onClick={() => activateAction(actionMatch.action)}
              onMouseEnter={() => setHighlightIndex(0)}
            >
              <Zap className="size-4 shrink-0 text-amber-500" />
              <span>{actionMatch.action.label}</span>
              {actionMatch.action.destructive && (
                <span className="text-xs text-destructive">(destructive)</span>
              )}
            </button>
          )}

          {showSearchResults && (
            <>
              {hasQuery && !hasResults && response && (
                <div className="px-3 py-6 text-center text-sm text-muted-foreground">
                  No results for &ldquo;{query}&rdquo;
                </div>
              )}

              <ul className="flex flex-col gap-3">
                {groups.map(({ items, type }) => (
                  <li key={type}>
                    <section>
                      <header className="px-3 py-1.5 text-xs font-medium text-muted-foreground uppercase">
                        <span>{typeLabels[type]}</span>
                      </header>

                      {items.map(({ index, result }) => (
                        <button
                          key={`${type}-${result.id}`}
                          data-active={index === highlightIndex || undefined}
                          className="flex w-full cursor-pointer items-center justify-between gap-2 px-3 py-1.5 text-left text-sm text-foreground hover:bg-accent/50 data-active:bg-accent data-active:text-accent-foreground"
                          onClick={() => selectItem({ type, result })}
                          onMouseEnter={() => setHighlightIndex(index)}
                        >
                          <span className="flex items-center gap-1.5 truncate font-medium">
                            {result.state && <StateOrb state={result.state} />}
                            <ResourceName name={result.name} />
                          </span>
                          {result.detail && (
                            <span className="truncate text-xs text-muted-foreground">
                              {result.detail}
                            </span>
                          )}
                        </button>
                      ))}
                    </section>
                  </li>
                ))}
              </ul>
            </>
          )}

          {activeAction &&
            currentStep &&
            (currentStep.type === "number" || currentStep.type === "text") && (
              <div className="px-3 py-6 text-center text-sm text-muted-foreground">
                {hasQuery ? (
                  <span>
                    Press <kbd className="rounded border px-1 py-0.5 text-xs">Enter</kbd> to confirm
                  </span>
                ) : (
                  <span>
                    {currentStep.placeholder ?? `Enter ${currentStep.label.toLowerCase()}`}
                  </span>
                )}
              </div>
            )}
        </div>

        {!activeAction && hasQuery && response && (
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
