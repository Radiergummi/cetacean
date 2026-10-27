import type { FetchResult, ListParams } from "../api/client";
import type { CollectionResponse } from "../api/types";
import type { SSEEvent } from "./useResourceStream";
import { useSearchParam } from "./useSearchParam";
import { useSortParams, type SortDir } from "./useSort";
import { useSwarmQuery } from "./useSwarmQuery";
import { useViewMode, type ViewMode } from "./useViewMode";
import { useCallback } from "react";

export interface UseListPageConfig<T> {
  path: string;
  sseType: string;
  defaultSort: string;
  viewModeKey: string;
  fetchFn: (params: ListParams, signal: AbortSignal) => Promise<FetchResult<CollectionResponse<T>>>;
  keyFn: (item: T) => string;
  /** An expr-lang `?filter=` the server applies before paging. */
  filter?: string | undefined;
  /**
   * The rows are derived from the streamed resources rather than being them,
   * so an event can't patch a row in place and every one refetches instead.
   */
  derived?: boolean | undefined;
  /**
   * Events this rejects leave the list alone; a sync always refetches. It is
   * handed the rows loaded so far.
   */
  relevantEvent?: ((event: SSEEvent, loaded: readonly T[]) => boolean) | undefined;
  /**
   * Told of each stream event the list acts on, so a view of it can follow too.
   */
  onEvent?: ((event: SSEEvent) => void) | undefined;
}

interface UseListPageResult<T> {
  data: T[];
  total: number;
  loading: boolean;
  error: Error | null;
  retry: () => void;
  hasMore: boolean;
  loadMore: () => void;
  allowedMethods: Set<string>;
  search: string;
  setSearch: (value: string) => void;
  sortKey: string | undefined;
  sortDir: SortDir;
  toggle: (key: string) => void;
  viewMode: ViewMode;
  setViewMode: (mode: ViewMode) => void;
}

/**
 * Encapsulates the common hook setup shared by all list pages:
 * search + debounce, server-side sort, paginated fetch with SSE updates,
 * and persisted view mode toggle.
 */
export function useListPage<T>({
  path,
  sseType,
  defaultSort,
  viewModeKey,
  fetchFn,
  keyFn,
  filter,
  derived,
  relevantEvent,
  onEvent,
}: UseListPageConfig<T>): UseListPageResult<T> {
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams(defaultSort);
  const { data, total, loading, error, retry, hasMore, loadMore, allowedMethods } = useSwarmQuery(
    [path, { search: debouncedSearch, sort: sortKey, dir: sortDir, filter }],
    useCallback(
      (offset: number, signal: AbortSignal) =>
        fetchFn({ search: debouncedSearch, sort: sortKey, dir: sortDir, filter, offset }, signal),
      [debouncedSearch, sortKey, sortDir, filter, fetchFn],
    ),
    sseType,
    keyFn,
    debouncedSearch !== "" || Boolean(filter) || Boolean(derived),
    relevantEvent,
    Boolean(derived),
    onEvent,
  );
  const [viewMode, setViewMode] = useViewMode(viewModeKey);

  return {
    data,
    total,
    loading,
    error,
    retry,
    hasMore,
    loadMore,
    allowedMethods,
    search,
    setSearch,
    sortKey,
    sortDir,
    toggle,
    viewMode,
    setViewMode,
  };
}
