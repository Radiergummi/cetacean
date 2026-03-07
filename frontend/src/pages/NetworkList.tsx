import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Network } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import DataTable from "../components/DataTable";
import type { Column } from "../components/DataTable";

const columns: Column<Network>[] = [
  { header: "Name", cell: (n) => n.Name },
  { header: "Driver", cell: (n) => n.Driver },
  { header: "Scope", cell: (n) => n.Scope },
];

export default function NetworkList() {
  const {
    data: networks,
    loading,
    error,
    retry,
  } = useSwarmResource(api.networks, "network", (n: Network) => n.Id);
  const [search, setSearch] = useSearchParam("q");
  const [viewMode, setViewMode] = useViewMode("networks");

  if (loading)
    return (
      <div>
        <PageHeader title="Networks" />
        <SkeletonTable columns={3} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  const filtered = networks.filter((n) => n.Name.toLowerCase().includes(search.toLowerCase()));

  return (
    <div>
      <PageHeader title="Networks" />
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search networks..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No networks match your search" : "No networks found"} />
      ) : viewMode === "table" ? (
        <DataTable columns={columns} data={filtered} keyFn={(n) => n.Id} />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((net) => (
            <div key={net.Id} className="rounded-lg border bg-card p-4">
              <div className="font-medium mb-2 truncate">{net.Name}</div>
              <div className="flex items-center gap-3 text-xs text-muted-foreground">
                <span>{net.Driver}</span>
                <span>{net.Scope}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
