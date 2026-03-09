import { useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSortParams } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Stack } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import DataTable, { type Column } from "../components/DataTable";
import SortIndicator from "../components/SortIndicator";
import ViewToggle from "../components/ViewToggle";
import ResourceCard from "../components/ResourceCard";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";

export default function StackList() {
  const [search, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("name");
  const {
    data: stacks,
    loading,
    error,
    retry,
  } = useSwarmResource(
    useCallback(
      () => api.stacks({ search, sort: sortKey, dir: sortDir }),
      [search, sortKey, sortDir],
    ),
    "stack",
    (s: Stack) => s.name,
  );
  const [viewMode, setViewMode] = useViewMode("stacks");
  const navigate = useNavigate();

  const columns: Column<Stack>[] = [
    {
      header: <SortIndicator label="Name" active={sortKey === "name"} dir={sortDir} />,
      cell: (stack) => (
        <Link
          to={`/stacks/${stack.name}`}
          className="text-link hover:underline font-medium"
          onClick={(e) => e.stopPropagation()}
        >
          {stack.name}
        </Link>
      ),
      onHeaderClick: () => toggle("name"),
    },
    { header: "Services", cell: (stack) => stack.services?.length || 0 },
    { header: "Configs", cell: (stack) => stack.configs?.length || 0 },
    { header: "Secrets", cell: (stack) => stack.secrets?.length || 0 },
    { header: "Networks", cell: (stack) => stack.networks?.length || 0 },
    { header: "Volumes", cell: (stack) => stack.volumes?.length || 0 },
  ];

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
      {stacks.length === 0 ? (
        <EmptyState message={search ? "No stacks match your search" : "No stacks found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={stacks}
          keyFn={(s) => s.name}
          onRowClick={(stack) => navigate(`/stacks/${stack.name}`)}
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {stacks.map((stack) => (
            <ResourceCard key={stack.name} title={stack.name} to={`/stacks/${stack.name}`}>
              <div className="grid grid-cols-3 gap-2 text-center">
                <CountBadge label="Services" count={stack.services?.length ?? 0} />
                <CountBadge label="Configs" count={stack.configs?.length ?? 0} />
                <CountBadge label="Secrets" count={stack.secrets?.length ?? 0} />
                <CountBadge label="Networks" count={stack.networks?.length ?? 0} />
                <CountBadge label="Volumes" count={stack.volumes?.length ?? 0} />
              </div>
            </ResourceCard>
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
