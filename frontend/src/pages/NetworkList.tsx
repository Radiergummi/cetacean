import { api } from "../api/client";
import type { Network } from "../api/types";
import DataTable, { type Column } from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import { SkeletonTable } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceCard from "../components/ResourceCard";
import ResourceName from "../components/ResourceName";
import SortIndicator from "../components/SortIndicator";
import { useSearchParam } from "../hooks/useSearchParam";
import { useSortParams } from "../hooks/useSort";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

export default function NetworkList() {
  const navigate = useNavigate();
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("name");
  const {
    data: networks,
    loading,
    error,
    retry,
    hasMore,
    loadMore,
  } = useSwarmResource(
    useCallback(
      (offset: number) =>
        api.networks({ search: debouncedSearch, sort: sortKey, dir: sortDir, offset }),
      [debouncedSearch, sortKey, sortDir],
    ),
    "network",
    ({ Id }: Network) => Id,
  );

  const columns: Column<Network>[] = useMemo(
    () => [
      {
        header: (
          <SortIndicator
            label="Name"
            active={sortKey === "name"}
            dir={sortDir}
          />
        ),
        cell: ({ Name }) => <ResourceName name={Name} />,
        onHeaderClick: () => toggle("name"),
      },
      {
        header: (
          <SortIndicator
            label="Driver"
            active={sortKey === "driver"}
            dir={sortDir}
          />
        ),
        cell: ({ Driver }) => Driver,
        onHeaderClick: () => toggle("driver"),
      },
      {
        header: (
          <SortIndicator
            label="Scope"
            active={sortKey === "scope"}
            dir={sortDir}
          />
        ),
        cell: ({ Scope }) => Scope,
        onHeaderClick: () => toggle("scope"),
      },
    ],
    [sortKey, sortDir, toggle],
  );
  const [viewMode, setViewMode] = useViewMode("networks");

  if (loading) {
    return (
      <div>
        <PageHeader title="Networks" />
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
      <PageHeader title="Networks" />

      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search networks…"
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />
      {networks.length === 0 ? (
        <EmptyState message={search ? "No networks match your search" : "No networks found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={networks}
          keyFn={({ Id }) => Id}
          onRowClick={({ Id }) => navigate(`/networks/${Id}`)}
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {networks.map(({ Driver, Id, Name, Scope }) => (
            <ResourceCard
              key={Id}
              title={<ResourceName name={Name} />}
              to={`/networks/${Id}`}
              meta={[Driver, Scope]}
            />
          ))}
        </div>
      )}
    </div>
  );
}
