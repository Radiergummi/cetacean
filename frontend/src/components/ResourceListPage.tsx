import { useListPage, type UseListPageConfig } from "../hooks/useListPage";
import type { SortDir } from "../hooks/useSort";
import type { ViewMode } from "../hooks/useViewMode";
import { cardGridClass } from "../lib/styles";
import DataTable, { type Column } from "./DataTable";
import EmptyState from "./EmptyState";
import FetchError from "./FetchError";
import ListToolbar from "./ListToolbar";
import { SkeletonTable } from "./LoadingSkeleton";
import LoadMoreSentinel from "./LoadMoreSentinel";
import PageHeader from "./PageHeader";
import { useEffect, useMemo, type ReactNode } from "react";
import { useNavigate } from "react-router-dom";

type ResourceListConfig<T> = UseListPageConfig<T> & {
  title: string;
  searchPlaceholder: string;
  itemPath: (item: T) => string;
  columns: (
    sortKey: string | undefined,
    sortDir: SortDir,
    toggle: (key: string) => void,
  ) => Column<T>[];
  emptyMessage: (hasSearch: boolean) => string;
  headerContent?: ReactNode | undefined;
  /**
   * Sits beneath the toolbar, handed the loaded rows and the collection's total.
   */
  belowToolbar?: ((items: T[], total: number) => ReactNode) | undefined;
  actions?: ((allowedMethods: Set<string>) => ReactNode) | undefined;
  skeletonColumns?: number | undefined;
  /**
   * Adds rows among the loaded items before both views render them, such as a
   * group's older versions beneath its newest. Keep it stable between renders.
   */
  arrange?: ((items: T[]) => T[]) | undefined;
  /**
   * Told each time the loaded items change, with how many fetches have
   * finished. Keep it stable between renders.
   */
  onItems?: ((items: T[], fetches: number) => void) | undefined;
} & (
    | { renderCard: (item: T) => ReactNode; renderGrid?: undefined }
    /**
     * Lays out the whole grid view, for a page that groups its cards.
     */
    | { renderGrid: (items: T[]) => ReactNode; renderCard?: undefined }
  );

export default function ResourceListPage<T>(config: ResourceListConfig<T>) {
  const { columns: columnsFn } = config;

  const {
    data: loaded,
    total,
    loading,
    error,
    retry,
    hasMore,
    loadMore,
    allowedMethods,
    fetches,
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

  const { arrange, onItems, belowToolbar, renderCard, renderGrid } = config;
  const data = useMemo(() => (arrange ? arrange(loaded) : loaded), [arrange, loaded]);

  useEffect(() => {
    onItems?.(loaded, fetches);
  }, [onItems, loaded, fetches]);

  return (
    <ListPageView
      title={config.title}
      searchPlaceholder={config.searchPlaceholder}
      itemPath={config.itemPath}
      keyFn={config.keyFn}
      columns={columns}
      emptyMessage={config.emptyMessage}
      headerContent={config.headerContent}
      belowToolbar={belowToolbar?.(loaded, total)}
      actions={config.actions?.(allowedMethods)}
      skeletonColumns={config.skeletonColumns}
      renderGrid={
        renderGrid ??
        ((items) => (
          <div className={cardGridClass}>
            {items.map((item) => (
              <div key={config.keyFn(item)}>{renderCard?.(item)}</div>
            ))}
          </div>
        ))
      }
      data={data}
      loading={loading}
      error={error}
      retry={retry}
      hasMore={hasMore}
      loadMore={loadMore}
      search={search}
      setSearch={setSearch}
      viewMode={viewMode}
      setViewMode={setViewMode}
    />
  );
}

interface ListPageViewProps<T> {
  title: string;
  searchPlaceholder: string;
  itemPath: (item: T) => string;
  keyFn: (item: T) => string;
  columns: Column<T>[];
  emptyMessage: (hasSearch: boolean) => string;
  headerContent?: ReactNode | undefined;
  belowToolbar?: ReactNode | undefined;
  actions?: ReactNode | undefined;
  skeletonColumns?: number | undefined;
  renderGrid: (items: T[]) => ReactNode;
  data: T[];
  loading: boolean;
  error: Error | null;
  retry: () => void;
  hasMore?: boolean | undefined;
  loadMore?: (() => void) | undefined;
  search: string;
  setSearch: (value: string) => void;
  viewMode: ViewMode;
  setViewMode: (mode: ViewMode) => void;
}

/**
 * The list page shell without a data source, for a list that is not a paged
 * collection of its own.
 */
export function ListPageView<T>({
  title,
  searchPlaceholder,
  itemPath,
  keyFn,
  columns,
  emptyMessage,
  headerContent,
  belowToolbar,
  actions,
  skeletonColumns,
  renderGrid,
  data,
  loading,
  error,
  retry,
  hasMore,
  loadMore,
  search,
  setSearch,
  viewMode,
  setViewMode,
}: ListPageViewProps<T>) {
  const navigate = useNavigate();

  if (loading) {
    return (
      <div>
        <PageHeader title={title} />
        <SkeletonTable columns={skeletonColumns ?? columns.length} />
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
        title={title}
        actions={actions}
      />
      {headerContent}
      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder={searchPlaceholder}
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />
      {belowToolbar}

      {data.length === 0 ? (
        <EmptyState message={emptyMessage(!!search)} />
      ) : viewMode === "table" ? (
        <DataTable
          label={title}
          columns={columns}
          data={data}
          keyFn={keyFn}
          onRowClick={(item) => navigate(itemPath(item))}
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      ) : (
        <>
          {renderGrid(data)}
          {hasMore && loadMore && (
            <LoadMoreSentinel
              key={data.length}
              onLoadMore={loadMore}
            />
          )}
        </>
      )}
    </div>
  );
}
