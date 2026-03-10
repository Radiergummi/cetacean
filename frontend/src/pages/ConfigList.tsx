import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSortParams } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Config } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import SortIndicator from "../components/SortIndicator";
import ViewToggle from "../components/ViewToggle";
import ResourceCard from "../components/ResourceCard";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import ResourceName from "../components/ResourceName";
import DataTable from "../components/DataTable";
import type { Column } from "../components/DataTable";
import TimeAgo from "../components/TimeAgo";

export default function ConfigList() {
  const navigate = useNavigate();
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("name");
  const {
    data: configs,
    loading,
    error,
    retry,
  } = useSwarmResource(
    useCallback(
      () => api.configs({ search: debouncedSearch, sort: sortKey, dir: sortDir }),
      [debouncedSearch, sortKey, sortDir],
    ),
    "config",
    (c: Config) => c.ID,
  );

  const columns: Column<Config>[] = [
    {
      header: <SortIndicator label="Name" active={sortKey === "name"} dir={sortDir} />,
      cell: (c) => <ResourceName name={c.Spec.Name || c.ID} />,
      onHeaderClick: () => toggle("name"),
    },
    {
      header: <SortIndicator label="Created" active={sortKey === "created"} dir={sortDir} />,
      cell: (c) => (c.CreatedAt ? <TimeAgo date={c.CreatedAt} /> : "\u2014"),
      onHeaderClick: () => toggle("created"),
    },
    {
      header: <SortIndicator label="Updated" active={sortKey === "updated"} dir={sortDir} />,
      cell: (c) => (c.UpdatedAt ? <TimeAgo date={c.UpdatedAt} /> : "\u2014"),
      onHeaderClick: () => toggle("updated"),
    },
  ];
  const [viewMode, setViewMode] = useViewMode("configs");

  if (loading)
    return (
      <div>
        <PageHeader title="Configs" />
        <SkeletonTable columns={3} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Configs" />
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search configs..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {configs.length === 0 ? (
        <EmptyState message={search ? "No configs match your search" : "No configs found"} />
      ) : viewMode === "table" ? (
        <DataTable columns={columns} data={configs} keyFn={(c) => c.ID} onRowClick={(c) => navigate(`/configs/${c.ID}`)} />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {configs.map((cfg) => (
            <ResourceCard key={cfg.ID} title={cfg.Spec.Name || cfg.ID} to={`/configs/${cfg.ID}`}>
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>Created: {cfg.CreatedAt ? <TimeAgo date={cfg.CreatedAt} /> : "\u2014"}</div>
                <div>Updated: {cfg.UpdatedAt ? <TimeAgo date={cfg.UpdatedAt} /> : "\u2014"}</div>
              </div>
            </ResourceCard>
          ))}
        </div>
      )}
    </div>
  );
}
