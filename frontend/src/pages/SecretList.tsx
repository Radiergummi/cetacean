import { useCallback } from "react";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Secret } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import DataTable from "../components/DataTable";
import type { Column } from "../components/DataTable";
import TimeAgo from "../components/TimeAgo";

const columns: Column<Secret>[] = [
  { header: "Name", cell: (s) => s.Spec.Name || s.ID },
  {
    header: "Created",
    cell: (s) => (s.CreatedAt ? <TimeAgo date={s.CreatedAt} /> : "\u2014"),
  },
  {
    header: "Updated",
    cell: (s) => (s.UpdatedAt ? <TimeAgo date={s.UpdatedAt} /> : "\u2014"),
  },
];

export default function SecretList() {
  const [search, setSearch] = useSearchParam("q");
  const {
    data: secrets,
    loading,
    error,
    retry,
  } = useSwarmResource(
    useCallback(() => api.secrets({ search }), [search]),
    "secret",
    (s: Secret) => s.ID,
  );
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
        <DataTable columns={columns} data={secrets} keyFn={(s) => s.ID} />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {secrets.map((secret) => (
            <div key={secret.ID} className="rounded-lg border bg-card p-4">
              <div className="font-medium mb-2 truncate">{secret.Spec.Name || secret.ID}</div>
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>
                  Created: {secret.CreatedAt ? <TimeAgo date={secret.CreatedAt} /> : "\u2014"}
                </div>
                <div>
                  Updated: {secret.UpdatedAt ? <TimeAgo date={secret.UpdatedAt} /> : "\u2014"}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
