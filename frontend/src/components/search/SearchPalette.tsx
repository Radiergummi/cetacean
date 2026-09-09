import { api, headAllowedMethods } from "../../api/client";
import type { SearchResourceType, SearchResponse, SearchResult } from "../../api/types";
import { getActions, matchAction, type PaletteAction, type PaletteStep } from "../../lib/actions";
import {
  flattenSearchResults,
  resourcePath,
  statusColor,
  typeLabels,
  typeOrder,
  type FlatSearchItem,
} from "../../lib/searchConstants";
import { showErrorToast } from "../../lib/showErrorToast";
import { getErrorMessage } from "../../lib/utils";
import ResourceName from "../ResourceName";
import { Spinner } from "../Spinner";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { ArrowRight, ChevronRight, Search, Zap } from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

function StateOrb({ state }: { state: string }) {
  if (state === "updating") {
    return <Spinner className="size-3 shrink-0 text-status-info" />;
  }

  const color = statusColor(state);

  return (
    <span
      className={`inline-block size-2 shrink-0 rounded-full ${color}`}
      title={state}
    />
  );
}

/** Map a singular resource type from action steps to plural SearchResourceType */
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
      {steps.map(({ label, type, resourceType }, index) => (
        <span
          key={label}
          className="flex items-center gap-1"
        >
          <ChevronRight className="size-3" />
          {index < currentStep ? (
            <span className="text-foreground">
              {type === "resource" ? (
                resourceType === "service" ? (
                  <ResourceName
                    name={
                      (actionArgs[index] as { name?: string })?.name ?? String(actionArgs[index])
                    }
                  />
                ) : (
                  ((actionArgs[index] as { name?: string })?.name ?? String(actionArgs[index]))
                )
              ) : (
                String(actionArgs[index])
              )}
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
  const [searching, setSearching] = useState(false);
  const [highlightIndex, setHighlightIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const paletteId = useId();
  const listboxId = `${paletteId}-listbox`;
  const optionId = useCallback((index: number) => `${paletteId}-option-${index}`, [paletteId]);
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
  const [pendingConfirm, setPendingConfirm] = useState<{
    action: PaletteAction;
    args: unknown[];
  } | null>(null);

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
    () => (response ? flattenSearchResults(response, resourceFilter) : []),
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
      setSearching(false);

      return;
    }

    const controller = new AbortController();
    abortRef.current = controller;
    setSearching(true);

    api
      .search(query, undefined, controller.signal)
      .then((r) => {
        if (!controller.signal.aborted) {
          setResponse(r);
          setHighlightIndex(0);
          setSearching(false);
        }
      })
      .catch((error) => {
        if (!controller.signal.aborted) {
          setSearching(false);
          showErrorToast(error, "Search failed");
        }
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
              const freshMap = new Map<string, { detail: string; state?: string | undefined }>();

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
    ({ result: { id }, type }: FlatSearchItem) => {
      navigate(resourcePath(type, id)!);
      onClose();
    },
    [navigate, onClose],
  );

  const activateAction = useCallback(
    (action: PaletteAction) => {
      if (action.steps.length === 0) {
        void action.execute();
        onClose();
        return;
      }

      setActiveAction(action);
      setActionStep(0);
      setActionArgs([]);
      setActionError(null);
      setQuery("");
      setResponse(null);
      setHighlightIndex(0);
    },
    [onClose],
  );

  const doExecute = useCallback(
    async (action: PaletteAction, args: unknown[]) => {
      setActionLoading(true);
      setActionError(null);

      try {
        await action.execute(...args);

        onClose();
      } catch (err) {
        setActionError(getErrorMessage(err, String(err)));
        showErrorToast(err, "Action failed");
      } finally {
        setActionLoading(false);
      }
    },
    [onClose],
  );

  const executeAction = useCallback(
    async (action: PaletteAction, args: unknown[]) => {
      // Check permission via HEAD before executing
      if (action.requiredMethod && action.steps[0]?.type === "resource") {
        const resource = args[0] as SearchResult;
        const path = resourcePath(action.steps[0].resourceType!, resource.id, resource.name);

        if (path) {
          const methods = await headAllowedMethods(path);

          if (!methods.has(action.requiredMethod)) {
            setActionError("You don't have permission to perform this action.");
            return;
          }
        }
      }

      if (action.destructive) {
        setPendingConfirm({ action, args });
        return;
      }

      void doExecute(action, args);
    },
    [doExecute],
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
    (item: FlatSearchItem) => {
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
          const stepValue = currentStep.type === "number" ? Number(query) : query;

          if (
            currentStep.type === "number" &&
            (isNaN(stepValue as number) || query.trim() === "")
          ) {
            return;
          }

          if (currentStep.type === "text" && query.trim() === "") {
            return;
          }

          advanceStep(stepValue);

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
  const groups = useMemo(() => {
    const result: { type: SearchResourceType; items: { index: number; result: SearchResult }[] }[] =
      [];
    let optionIndex = actionSuggestionOffset;
    const typesToRender = resourceFilter ? [resourceFilter] : typeOrder;

    for (const type of typesToRender) {
      const results = response?.results[type];

      if (results && results.length > 0) {
        const items = results.map((result) => ({ index: optionIndex++, result }));

        result.push({ type, items });
      }
    }

    return result;
  }, [response, resourceFilter, actionSuggestionOffset]);

  const hasQuery = query.trim().length > 0;
  const hasResults = flat.length > 0;
  const showSearchResults = currentStep?.type === "resource" || !activeAction;

  const listboxOpen = showSearchResults && (totalItems > 0 || (hasQuery && hasResponse));

  return (
    <Dialog
      open
      onOpenChange={(next, details) => {
        if (next) {
          return;
        }

        // Inside a multi-step action, Escape means "back one step", not
        // "close" — so the dismissal is cancelled and the step unwound.
        if (details.reason === "escape-key" && activeAction) {
          details.cancel();
          goBackStep();

          return;
        }

        onClose();
      }}
    >
      <DialogContent
        showCloseButton={false}
        aria-label="Search"
        className="top-[5vh] max-w-lg translate-y-0 gap-0 p-0 sm:max-w-lg md:top-[15vh]"
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
          {/*
            The APG combobox contract: focus never leaves this input, and the
            row it is pointing at is published through aria-activedescendant.
            Without it, arrowing through results was silent — the highlight was
            a colour and nothing else.
          */}
          <input
            ref={inputRef}
            type={currentStep?.type === "number" ? "number" : "text"}
            role="combobox"
            aria-expanded={listboxOpen}
            aria-controls={listboxId}
            aria-autocomplete="list"
            aria-activedescendant={
              listboxOpen && highlightIndex >= 0 ? optionId(highlightIndex) : undefined
            }
            aria-label={placeholder}
            value={query}
            onChange={(event) => onInputChange(event.target.value)}
            placeholder={placeholder}
            className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
          {(searching || actionLoading) && <Spinner className="size-4 shrink-0" />}
        </div>

        {actionError && (
          <div className="border-b bg-destructive/10 px-3 py-2 text-xs text-destructive">
            {actionError}
          </div>
        )}

        {pendingConfirm && (
          <div className="flex items-center justify-between border-b bg-destructive/5 px-3 py-2.5">
            <span className="text-sm text-foreground">Confirm: {pendingConfirm.action.label}?</span>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setPendingConfirm(null)}
                className="rounded-md px-2.5 py-1 text-xs font-medium text-muted-foreground hover:bg-muted"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => {
                  const { action, args } = pendingConfirm;
                  setPendingConfirm(null);
                  void doExecute(action, args);
                }}
                className="rounded-md bg-destructive/10 px-2.5 py-1 text-xs font-medium text-destructive hover:bg-destructive/20 dark:bg-destructive/20 dark:hover:bg-destructive/30"
              >
                Confirm
              </button>
            </div>
          </div>
        )}

        <div className="max-h-72 overflow-y-auto">
          {showSearchResults && hasQuery && !hasResults && response && (
            <div className="px-3 py-6 text-center text-sm text-muted-foreground">
              No results for &ldquo;{query}&rdquo;
            </div>
          )}

          {/*
            One listbox holds every highlightable row, the action suggestion
            included, because `highlightIndex` numbers them in one sequence and
            aria-activedescendant has to resolve inside the listbox it names.
            Rows are options rather than buttons: a button would be its own tab
            stop, so Tab would walk the results instead of leaving the palette.
          */}
          <ul
            id={listboxId}
            role="listbox"
            aria-label="Search results"
            className="flex flex-col gap-3"
          >
            {actionMatch &&
              !activeAction && (
                // Focus stays in the combobox input, which owns the arrow keys
                // and Enter; an option in this pattern is pointed at, never
                // focused, so it carries no key handler of its own.
                // oxlint-disable-next-line jsx-a11y/click-events-have-key-events
                <li
                  id={optionId(0)}
                  role="option"
                  aria-selected={highlightIndex === 0}
                  data-active={highlightIndex === 0 || undefined}
                  className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left text-sm font-medium text-foreground hover:bg-accent/50 data-active:bg-accent data-active:text-accent-foreground"
                  onClick={() => activateAction(actionMatch.action)}
                  onMouseEnter={() => setHighlightIndex(0)}
                >
                  <Zap className="size-4 shrink-0 text-amber-500" />
                  <span>{actionMatch.action.label}</span>
                </li>
              )}

            {showSearchResults &&
              groups.map(({ items, type }) => (
                <li
                  key={type}
                  role="group"
                  aria-label={typeLabels[type]}
                >
                  <span
                    aria-hidden="true"
                    className="block px-3 py-1.5 text-xs font-medium text-muted-foreground uppercase"
                  >
                    {typeLabels[type]}
                  </span>

                  {items.map(({ index, result }) => (
                    // Pointed at through aria-activedescendant rather than
                    // focused — see the note on the listbox above.
                    // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/interactive-supports-focus
                    <span
                      key={`${type}-${result.id}`}
                      id={optionId(index)}
                      role="option"
                      aria-selected={index === highlightIndex}
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
                    </span>
                  ))}
                </li>
              ))}
          </ul>

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

        <div
          className="sr-only"
          aria-live="polite"
          role="status"
        >
          {response && hasQuery ? `${response.total} result${response.total !== 1 ? "s" : ""}` : ""}
        </div>

        {!activeAction && hasQuery && response && (
          <div className="flex items-center justify-between border-t px-3 py-2 text-xs text-muted-foreground">
            <span>
              {response.total} result{response.total !== 1 ? "s" : ""}
            </span>
            <button
              type="button"
              className="text-primary hover:underline"
              onClick={() => {
                navigate(`/search?q=${encodeURIComponent(query)}`);
                onClose();
              }}
            >
              View all results <ArrowRight className="ml-1 inline size-3" />
            </button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
