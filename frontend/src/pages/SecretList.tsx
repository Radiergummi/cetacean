import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSortParams } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Secret } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import SortIndicator from "../components/SortIndicator";
import ViewToggle from "../components/ViewToggle";
import ResourceCard from "../components/ResourceCard";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import DataTable from "../components/DataTable";
import type { Column } from "../components/DataTable";
import TimeAgo from "../components/TimeAgo";

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
      header: <SortIndicator label="Name" active={sortKey === "name"} dir={sortDir} />,
      cell: (s) => s.Spec.Name || s.ID,
      onHeaderClick: () => toggle("name"),
    },
    {
      header: <SortIndicator label="Created" active={sortKey === "created"} dir={sortDir} />,
      cell: (s) => (s.CreatedAt ? <TimeAgo date={s.CreatedAt} /> : "\u2014"),
      onHeaderClick: () => toggle("created"),
    },
    {
      header: <SortIndicator label="Updated" active={sortKey === "updated"} dir={sortDir} />,
      cell: (s) => (s.UpdatedAt ? <TimeAgo date={s.UpdatedAt} /> : "\u2014"),
      onHeaderClick: () => toggle("updated"),
    },
  ];
  const [viewMode, setViewMode] = useViewMode("secrets");

  if (loading)
    return (
      <div>
        <PageHeader title="Secrets" />
        <SkeletonTable columns={3} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Secrets" />
      <p className="text-sm text-muted-foreground mb-4">
        Metadata only. Secret values are never exposed.
      </p>
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search secrets..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {secrets.length === 0 ? (
        <EmptyState message={search ? "No secrets match your search" : "No secrets found"} />
      ) : viewMode === "table" ? (
        <DataTable columns={columns} data={secrets} keyFn={(s) => s.ID} onRowClick={(s) => navigate(`/secrets/${s.ID}`)} />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {secrets.map((secret) => (
            <ResourceCard key={secret.ID} title={secret.Spec.Name || secret.ID} to={`/secrets/${secret.ID}`}>
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>Created: {secret.CreatedAt ? <TimeAgo date={secret.CreatedAt} /> : "\u2014"}</div>
                <div>Updated: {secret.UpdatedAt ? <TimeAgo date={secret.UpdatedAt} /> : "\u2014"}</div>
              </div>
            </ResourceCard>
          ))}
        </div>
      )}
    </div>
  );
}
