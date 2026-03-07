import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSort } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Stack } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import SortableHeader from "../components/SortableHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";

const sortAccessors = {
  name: (s: Stack) => s.name,
  services: (s: Stack) => s.services?.length ?? 0,
  configs: (s: Stack) => s.configs?.length ?? 0,
  secrets: (s: Stack) => s.secrets?.length ?? 0,
  networks: (s: Stack) => s.networks?.length ?? 0,
  volumes: (s: Stack) => s.volumes?.length ?? 0,
};

export default function StackList() {
  const {
    data: stacks,
    loading,
    error,
    retry,
  } = useSwarmResource(api.stacks, "stack", (s: Stack) => s.name);
  const [search, setSearch] = useSearchParam("q");
  const [viewMode, setViewMode] = useViewMode("stacks");
  const navigate = useNavigate();
  const filtered = useMemo(
    () => stacks.filter((s) => s.name.toLowerCase().includes(search.toLowerCase())),
    [stacks, search],
  );
  const { sorted, sortKey, sortDir, toggle } = useSort(filtered, sortAccessors, "name");

  if (loading)
    return (
      <div>
        <PageHeader title="Stacks" />
        <SkeletonTable columns={6} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Stacks" />
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search stacks..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {sorted.length === 0 ? (
        <EmptyState message={search ? "No stacks match your search" : "No stacks found"} />
      ) : viewMode === "table" ? (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full">
            <thead className="sticky top-0 z-10 bg-background">
              <tr className="border-b bg-muted/50">
                <SortableHeader
                  label="Name"
                  sortKey="name"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Services"
                  sortKey="services"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Configs"
                  sortKey="configs"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Secrets"
                  sortKey="secrets"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Networks"
                  sortKey="networks"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Volumes"
                  sortKey="volumes"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
              </tr>
            </thead>
            <tbody>
              {sorted.map((stack) => (
                <tr
                  key={stack.name}
                  className="border-b cursor-pointer hover:bg-muted/50"
                  onClick={() => navigate(`/stacks/${stack.name}`)}
                >
                  <td className="p-3">
                    <Link
                      to={`/stacks/${stack.name}`}
                      className="text-link hover:underline font-medium"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {stack.name}
                    </Link>
                  </td>
                  <td className="p-3 text-sm">{stack.services?.length || 0}</td>
                  <td className="p-3 text-sm">{stack.configs?.length || 0}</td>
                  <td className="p-3 text-sm">{stack.secrets?.length || 0}</td>
                  <td className="p-3 text-sm">{stack.networks?.length || 0}</td>
                  <td className="p-3 text-sm">{stack.volumes?.length || 0}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {sorted.map((stack) => (
            <Link
              key={stack.name}
              to={`/stacks/${stack.name}`}
              className="rounded-lg border bg-card p-4 hover:border-foreground/20 hover:shadow-sm transition-all"
            >
              <div className="font-medium mb-3">{stack.name}</div>
              <div className="grid grid-cols-3 gap-2 text-center">
                <CountBadge label="Services" count={stack.services?.length ?? 0} />
                <CountBadge label="Configs" count={stack.configs?.length ?? 0} />
                <CountBadge label="Secrets" count={stack.secrets?.length ?? 0} />
                <CountBadge label="Networks" count={stack.networks?.length ?? 0} />
                <CountBadge label="Volumes" count={stack.volumes?.length ?? 0} />
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

function CountBadge({ label, count }: { label: string; count: number }) {
  return (
    <div>
      <div className="text-sm font-semibold tabular-nums">{count}</div>
      <div className="text-[10px] text-muted-foreground">{label}</div>
    </div>
  );
}
