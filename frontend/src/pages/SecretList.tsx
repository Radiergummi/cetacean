import { api } from "../api/client";
import type { Secret } from "../api/types";
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
import { useCallback } from "react";
import { useNavigate } from "react-router-dom";

export default function SecretList() {
  const navigate = useNavigate();
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("name");
  const {
    data: secrets,
    loading,
    error,
    retry,
  } = useSwarmResource(
    useCallback(
      () => api.secrets({ search: debouncedSearch, sort: sortKey, dir: sortDir }),
      [debouncedSearch, sortKey, sortDir],
    ),
    "secret",
    (s: Secret) => s.ID,
  );

  const columns: Column<Secret>[] = [
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
  ];
  const [viewMode, setViewMode] = useViewMode("secrets");

  if (loading) {
    return (
      <div>
        <PageHeader title="Secrets" />
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
        title="Secrets"
        actions={
          <CreateDataResourceForm
            resourceType="Secret"
            basePath="/secrets"
            onCreate={async (name, data) => {
              const response = await api.createSecret(name, data);
              return { id: response.secret.ID };
            }}
          />
        }
      />

      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search secrets…"
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />

      {secrets.length === 0 ? (
        <EmptyState message={search ? "No secrets match your search" : "No secrets found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={secrets}
          keyFn={({ ID }) => ID}
          onRowClick={({ ID }) => navigate(`/secrets/${ID}`)}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {secrets.map(({ CreatedAt, ID, Spec: { Name }, UpdatedAt }) => (
            <ResourceCard
              key={ID}
              title={<ResourceName name={Name || ID} />}
              to={`/secrets/${ID}`}
            >
              <div className="flex flex-col gap-1 text-xs text-muted-foreground">
                <span>Created: {CreatedAt ? <TimeAgo date={CreatedAt} /> : "\u2014"}</span>
                <span>Updated: {UpdatedAt ? <TimeAgo date={UpdatedAt} /> : "\u2014"}</span>
              </div>
            </ResourceCard>
          ))}
        </div>
      )}
    </div>
  );
}
