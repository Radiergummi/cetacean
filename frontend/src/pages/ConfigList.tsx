import { api } from "../api/client";
import type { Config } from "../api/types";
import CreateDataResourceForm from "../components/CreateDataResourceForm";
import DataTable, { type Column } from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import { SkeletonTable } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceCard from "../components/ResourceCard";
import ResourceName from "../components/ResourceName";
import SortIndicator from "../components/SortIndicator";
import TimeAgo from "../components/TimeAgo";
import { useSearchParam } from "../hooks/useSearchParam";
import { useSortParams } from "../hooks/useSort";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { useCallback, useMemo } from "react";
import { useNavigate } from "react-router-dom";

export default function ConfigList() {
  const navigate = useNavigate();
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("name");
  const {
    data: configs,
    loading,
    error,
    retry,
    hasMore,
    loadMore,
    allowedMethods,
  } = useSwarmResource(
    useCallback(
      (offset: number, signal: AbortSignal) =>
        api.configs({ search: debouncedSearch, sort: sortKey, dir: sortDir, offset }, signal),
      [debouncedSearch, sortKey, sortDir],
    ),
    "config",
    ({ ID }: Config) => ID,
  );

  const columns: Column<Config>[] = useMemo(
    () => [
      {
        header: (
          <SortIndicator
            label="Name"
            active={sortKey === "name"}
            dir={sortDir}
          />
        ),
        cell: ({ ID, Spec: { Name } }) => <ResourceName name={Name || ID} />,
        onHeaderClick: () => toggle("name"),
      },
      {
        header: (
          <SortIndicator
            label="Created"
            active={sortKey === "created"}
            dir={sortDir}
          />
        ),
        cell: ({ CreatedAt }) => (CreatedAt ? <TimeAgo date={CreatedAt} /> : "\u2014"),
        onHeaderClick: () => toggle("created"),
      },
      {
        header: (
          <SortIndicator
            label="Updated"
            active={sortKey === "updated"}
            dir={sortDir}
          />
        ),
        cell: ({ UpdatedAt }) => (UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "\u2014"),
        onHeaderClick: () => toggle("updated"),
      },
    ],
    [sortKey, sortDir, toggle],
  );

  const [viewMode, setViewMode] = useViewMode("configs");

  if (loading) {
    return (
      <div>
        <PageHeader title="Configs" />
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
      <PageHeader
        title="Configs"
        actions={
          <CreateDataResourceForm
            resourceType="Config"
            basePath="/configs"
            canCreate={allowedMethods.has("POST")}
            onCreate={async (name, data) => {
              const response = await api.createConfig(name, data);
              return { id: response.config.ID };
            }}
          />
        }
      />
      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search configs…"
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />

      {configs.length === 0 ? (
        <EmptyState message={search ? "No configs match your search" : "No configs found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={configs}
          keyFn={({ ID }) => ID}
          onRowClick={({ ID }) => navigate(`/configs/${ID}`)}
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {configs.map(({ CreatedAt, ID, Spec: { Name }, UpdatedAt }) => (
            <ResourceCard
              key={ID}
              title={<ResourceName name={Name || ID} />}
              to={`/configs/${ID}`}
            >
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>Created: {CreatedAt ? <TimeAgo date={CreatedAt} /> : "\u2014"}</div>
                <div>Updated: {UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "\u2014"}</div>
              </div>
            </ResourceCard>
          ))}
        </div>
      )}
    </div>
  );
}
