import type { FetchResult, ListParams } from "../api/client";
import type { CollectionResponse } from "../api/types";
import { useSearchParam } from "../hooks/useSearchParam";
import { useSortParams } from "../hooks/useSort";
import type { SortDir } from "../hooks/useSort";
import { useSwarmQuery } from "../hooks/useSwarmQuery";
import { useViewMode } from "../hooks/useViewMode";
import { cardGridClass } from "../lib/styles";
import DataTable, { type Column } from "./DataTable";
import EmptyState from "./EmptyState";
import FetchError from "./FetchError";
import ListToolbar from "./ListToolbar";
import { SkeletonTable } from "./LoadingSkeleton";
import PageHeader from "./PageHeader";
import { useCallback, useMemo, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";

interface ResourceListConfig<T> {
  title: string;
  path: string;
  sseType: string;
  defaultSort: string;
  searchPlaceholder: string;
  viewModeKey: string;
  fetchFn: (params: ListParams, signal: AbortSignal) => Promise<FetchResult<CollectionResponse<T>>>;
  keyFn: (item: T) => string;
  itemPath: (item: T) => string;
  columns: (
    sortKey: string | undefined,
    sortDir: SortDir,
    toggle: (key: string) => void,
  ) => Column<T>[];
  renderCard: (item: T) => ReactNode;
  emptyMessage: (hasSearch: boolean) => string;
  actions?: (allowedMethods: Set<string>) => ReactNode;
  skeletonColumns?: number;
}

export default function ResourceListPage<T>(config: ResourceListConfig<T>) {
  const navigate = useNavigate();
  const { fetchFn, columns: columnsFn, path, sseType, keyFn } = config;
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams(config.defaultSort);
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

  const columns = useMemo(
    () => columnsFn(sortKey, sortDir, toggle),
    [sortKey, sortDir, toggle, columnsFn],
  );

  const [viewMode, setViewMode] = useViewMode(config.viewModeKey);

  if (loading) {
    return (
      <div>
        <PageHeader title={config.title} />
        <SkeletonTable columns={config.skeletonColumns ?? columns.length} />
      </div>
    );
  }

  if (error) {
    return (
      <FetchError
        message={error.message}
        onRetry={retry}
      />
    );
  }

  return (
    <div>
      <PageHeader
        title={config.title}
        actions={config.actions?.(allowedMethods)}
      />
      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder={config.searchPlaceholder}
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />

      {data.length === 0 ? (
        <EmptyState message={config.emptyMessage(!!search)} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={data}
          keyFn={config.keyFn}
          onRowClick={(item) => navigate(config.itemPath(item))}
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      ) : (
        <div className={cardGridClass}>
          {data.map((item) => (
            <div key={config.keyFn(item)}>{config.renderCard(item)}</div>
          ))}
        </div>
      )}
    </div>
  );
}
