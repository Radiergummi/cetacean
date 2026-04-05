import type { FetchResult, ListParams } from "../api/client";
import type { CollectionResponse } from "../api/types";
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
}

interface UseListPageResult<T> {
  data: T[];
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
}: UseListPageConfig<T>): UseListPageResult<T> {
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams(defaultSort);
  const { data, loading, error, retry, hasMore, loadMore, allowedMethods } = useSwarmQuery(
    [path, { search: debouncedSearch, sort: sortKey, dir: sortDir }],
    useCallback(
      (offset: number, signal: AbortSignal) =>
        fetchFn({ search: debouncedSearch, sort: sortKey, dir: sortDir, offset }, signal),
      [debouncedSearch, sortKey, sortDir, fetchFn],
    ),
    sseType,
    keyFn,
  );
  const [viewMode, setViewMode] = useViewMode(viewModeKey);

  return {
    data,
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
