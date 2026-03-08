import { useCallback } from "react";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Config } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import DataTable from "../components/DataTable";
import type { Column } from "../components/DataTable";
import TimeAgo from "../components/TimeAgo";

const columns: Column<Config>[] = [
  { header: "Name", cell: (c) => c.Spec.Name || c.ID },
  {
    header: "Created",
    cell: (c) => (c.CreatedAt ? <TimeAgo date={c.CreatedAt} /> : "\u2014"),
  },
  {
    header: "Updated",
    cell: (c) => (c.UpdatedAt ? <TimeAgo date={c.UpdatedAt} /> : "\u2014"),
  },
];

export default function ConfigList() {
  const [search, setSearch] = useSearchParam("q");
  const {
    data: configs,
    loading,
    error,
    retry,
  } = useSwarmResource(
    useCallback(() => api.configs({ search }), [search]),
    "config",
    (c: Config) => c.ID,
  );
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
        <DataTable columns={columns} data={configs} keyFn={(c) => c.ID} />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {configs.map((cfg) => (
            <div key={cfg.ID} className="rounded-lg border bg-card p-4">
              <div className="font-medium mb-2 truncate">{cfg.Spec.Name || cfg.ID}</div>
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>Created: {cfg.CreatedAt ? <TimeAgo date={cfg.CreatedAt} /> : "\u2014"}</div>
                <div>Updated: {cfg.UpdatedAt ? <TimeAgo date={cfg.UpdatedAt} /> : "\u2014"}</div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
