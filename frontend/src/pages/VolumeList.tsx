import { api } from "../api/client";
import type { Volume } from "../api/types";
import DataTable, { type Column } from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import { SkeletonTable } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceCard from "../components/ResourceCard";
import ResourceName from "../components/ResourceName";
import { useSearchParam } from "../hooks/useSearchParam";
import { sortColumn } from "../lib/sortColumn";
import { useSortParams } from "../hooks/useSort";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

export default function VolumeList() {
  const navigate = useNavigate();
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("name");
  const {
    data: volumes,
    loading,
    error,
    retry,
    hasMore,
    loadMore,
  } = useSwarmResource(
    useCallback(
      (offset: number, signal: AbortSignal) =>
        api.volumes({ search: debouncedSearch, sort: sortKey, dir: sortDir, offset }, signal),
      [debouncedSearch, sortKey, sortDir],
    ),
    "volume",
    ({ Name }: Volume) => Name,
  );

  const columns: Column<Volume>[] = useMemo(
    () => [
      {
        ...sortColumn("Name", "name", sortKey, sortDir, toggle),
        cell: ({ Name }) => <ResourceName name={Name} />,
      },
      {
        ...sortColumn("Driver", "driver", sortKey, sortDir, toggle),
        cell: ({ Driver }) => Driver,
      },
      {
        ...sortColumn("Scope", "scope", sortKey, sortDir, toggle),
        cell: ({ Scope }) => Scope,
      },
    ],
    [sortKey, sortDir, toggle],
  );
  const [viewMode, setViewMode] = useViewMode("volumes");

  if (loading) {
    return (
      <div>
        <PageHeader title="Volumes" />
        <SkeletonTable columns={3} />
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
      <PageHeader title="Volumes" />
      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search volumes…"
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />
      {volumes.length === 0 ? (
        <EmptyState message={search ? "No volumes match your search" : "No volumes found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={volumes}
          keyFn={({ Name }) => Name}
          onRowClick={({ Name }) => navigate(`/volumes/${Name}`)}
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {volumes.map(({ Driver, Name, Scope }) => (
            <ResourceCard
              key={Name}
              title={<ResourceName name={Name} />}
              to={`/volumes/${Name}`}
              meta={[Driver, Scope]}
            />
          ))}
        </div>
      )}
    </div>
  );
}
