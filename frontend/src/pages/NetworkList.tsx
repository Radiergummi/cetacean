import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSortParams } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Network } from "../api/types";
import ListToolbar from "../components/ListToolbar";
import PageHeader from "../components/PageHeader";
import SortIndicator from "../components/SortIndicator";
import ResourceCard from "../components/ResourceCard";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import ResourceName from "../components/ResourceName";
import DataTable, { type Column } from "../components/DataTable";

export default function NetworkList() {
  const navigate = useNavigate();
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("name");
  const {
    data: networks,
    loading,
    error,
    retry,
  } = useSwarmResource(
    useCallback(
      () => api.networks({ search: debouncedSearch, sort: sortKey, dir: sortDir }),
      [debouncedSearch, sortKey, sortDir],
    ),
    "network",
    (n: Network) => n.Id,
  );

  const columns: Column<Network>[] = [
    {
      header: <SortIndicator label="Name" active={sortKey === "name"} dir={sortDir} />,
      cell: (n) => <ResourceName name={n.Name} />,
      onHeaderClick: () => toggle("name"),
    },
    {
      header: <SortIndicator label="Driver" active={sortKey === "driver"} dir={sortDir} />,
      cell: (n) => n.Driver,
      onHeaderClick: () => toggle("driver"),
    },
    {
      header: <SortIndicator label="Scope" active={sortKey === "scope"} dir={sortDir} />,
      cell: (n) => n.Scope,
      onHeaderClick: () => toggle("scope"),
    },
  ];
  const [viewMode, setViewMode] = useViewMode("networks");

  if (loading)
    return (
      <div>
        <PageHeader title="Networks" />
        <SkeletonTable columns={3} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Networks" />
      <ListToolbar search={search} onSearchChange={setSearch} placeholder="Search networks..." viewMode={viewMode} onViewModeChange={setViewMode} />
      {networks.length === 0 ? (
        <EmptyState message={search ? "No networks match your search" : "No networks found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={networks}
          keyFn={(n) => n.Id}
          onRowClick={(n) => navigate(`/networks/${n.Id}`)}
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {networks.map((net) => (
            <ResourceCard
              key={net.Id}
              title={<ResourceName name={net.Name} />}
              to={`/networks/${net.Id}`}
              meta={[net.Driver, net.Scope]}
            />
          ))}
        </div>
      )}
    </div>
  );
}
