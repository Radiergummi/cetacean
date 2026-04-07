import { useListPage, type UseListPageConfig } from "../hooks/useListPage";
import type { SortDir } from "../hooks/useSort";
import { cardGridClass } from "../lib/styles";
import DataTable, { type Column } from "./DataTable";
import EmptyState from "./EmptyState";
import FetchError from "./FetchError";
import ListToolbar from "./ListToolbar";
import { SkeletonTable } from "./LoadingSkeleton";
import PageHeader from "./PageHeader";
import { useMemo, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";

interface ResourceListConfig<T> extends UseListPageConfig<T> {
  title: string;
  searchPlaceholder: string;
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
  const { columns: columnsFn } = config;

  const {
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
  } = useListPage(config);

  const columns = useMemo(
    () => columnsFn(sortKey, sortDir, toggle),
    [sortKey, sortDir, toggle, columnsFn],
  );

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
