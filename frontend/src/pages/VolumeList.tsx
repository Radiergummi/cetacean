import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Volume } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import DataTable from "../components/DataTable";
import type { Column } from "../components/DataTable";

const columns: Column<Volume>[] = [
  { header: "Name", cell: (v) => v.Name },
  { header: "Driver", cell: (v) => v.Driver },
  { header: "Scope", cell: (v) => v.Scope },
];

export default function VolumeList() {
  const {
    data: volumes,
    loading,
    error,
    retry,
  } = useSwarmResource(api.volumes, "volume", (v: Volume) => v.Name);
  const [search, setSearch] = useSearchParam("q");
  const [viewMode, setViewMode] = useViewMode("volumes");

  if (loading)
    return (
      <div>
        <PageHeader title="Volumes" />
        <SkeletonTable columns={3} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  const filtered = volumes.filter((v) => v.Name.toLowerCase().includes(search.toLowerCase()));

  return (
    <div>
      <PageHeader title="Volumes" />
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search volumes..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No volumes match your search" : "No volumes found"} />
      ) : viewMode === "table" ? (
        <DataTable columns={columns} data={filtered} keyFn={(v) => v.Name} />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((vol) => (
            <div key={vol.Name} className="rounded-lg border bg-card p-4">
              <div className="font-medium mb-2 truncate">{vol.Name}</div>
              <div className="flex items-center gap-3 text-xs text-muted-foreground">
                <span>{vol.Driver}</span>
                <span>{vol.Scope}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
