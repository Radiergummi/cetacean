import { useListPage, type UseListPageConfig } from "../hooks/useListPage";
import type { SortDir } from "../hooks/useSort";
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
  headerContent?: ReactNode | undefined;
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
}

export default function ResourceListPage<T>(config: ResourceListConfig<T>) {
  const navigate = useNavigate();
  const { columns: columnsFn } = config;

  const {
    data: loaded,
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

  const { arrange, onItems } = config;
  const data = useMemo(() => (arrange ? arrange(loaded) : loaded), [arrange, loaded]);

  useEffect(() => {
    onItems?.(loaded, fetches);
  }, [onItems, loaded, fetches]);

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
      {config.headerContent}
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
          label={config.title}
          columns={columns}
          data={data}
          keyFn={config.keyFn}
          onRowClick={(item) => navigate(config.itemPath(item))}
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      ) : (
        <>
          <div className={cardGridClass}>
            {data.map((item) => (
              <div key={config.keyFn(item)}>{config.renderCard(item)}</div>
            ))}
          </div>
          {hasMore && (
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
