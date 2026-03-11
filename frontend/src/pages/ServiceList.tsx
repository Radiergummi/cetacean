import type React from "react";
import { useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSortParams } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { ServiceListItem } from "../api/types";
import { SearchInput } from "../components/search";
import PageHeader from "../components/PageHeader";
import DataTable, { type Column } from "../components/DataTable";
import SortIndicator from "../components/SortIndicator";
import ViewToggle from "../components/ViewToggle";
import ResourceCard from "../components/ResourceCard";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import ResourceName from "../components/ResourceName";

function ReplicaHealth({ running, desired }: { running: number; desired: number }) {
  const healthy = running >= desired && desired > 0;

  return (
    <span
      data-healthy={healthy || undefined}
      className="tabular-nums font-medium text-red-600 dark:text-red-400 data-healthy:text-green-600 dark:data-healthy:text-green-400"
    >
      {running}/{desired}
    </span>
  );
}

export default function ServiceList() {
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("name");
  const {
    data: services,
    loading,
    error,
    retry,
  } = useSwarmResource(
    useCallback(
      () => api.services({ search: debouncedSearch, sort: sortKey, dir: sortDir }),
      [debouncedSearch, sortKey, sortDir],
    ),
    "service",
    (s: ServiceListItem) => s.ID,
  );
  const [viewMode, setViewMode] = useViewMode("services");
  const navigate = useNavigate();

  const columns: Column<ServiceListItem>[] = [
    {
      header: <SortIndicator label="Name" active={sortKey === "name"} dir={sortDir} />,
      cell: (svc) => (
        <Link
          to={`/services/${svc.ID}`}
          className="text-link hover:underline font-medium"
          onClick={(e) => e.stopPropagation()}
        >
          <ResourceName name={svc.Spec.Name} />
        </Link>
      ),
      onHeaderClick: () => toggle("name"),
    },
    {
      header: "Image",
      cell: (svc) => (
        <span className="font-mono text-xs">
          {svc.Spec.TaskTemplate.ContainerSpec.Image.split("@")[0]}
        </span>
      ),
    },
    {
      header: <SortIndicator label="Mode" active={sortKey === "mode"} dir={sortDir} />,
      cell: (svc) => (svc.Spec.Mode.Replicated ? "replicated" : "global"),
      onHeaderClick: () => toggle("mode"),
    },
    {
      header: "Replicas",
      cell: (svc) => {
        const desired = svc.Spec.Mode.Replicated?.Replicas;
        if (desired == null) return "\u2014";
        return <ReplicaHealth running={svc.RunningTasks} desired={desired} />;
      },
    },
    {
      header: "Update Status",
      cell: (svc) => svc.UpdateStatus?.State || "\u2014",
    },
  ];

  if (loading)
    return (
      <div>
        <PageHeader title="Services" />
        <SkeletonTable columns={5} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Services" />
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search services..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {services.length === 0 ? (
        <EmptyState message={search ? "No services match your search" : "No services found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={services}
          keyFn={(svc) => svc.ID}
          onRowClick={(svc) => navigate(`/services/${svc.ID}`)}
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {services.map((svc) => {
            const desired = svc.Spec.Mode.Replicated?.Replicas;
            return (
              <ResourceCard
                key={svc.ID}
                title={<ResourceName name={svc.Spec.Name} />}
                to={`/services/${svc.ID}`}
                subtitle={svc.Spec.TaskTemplate.ContainerSpec.Image.split("@")[0]}
                meta={
                  [
                    svc.Spec.Mode.Replicated ? "replicated" : "global",
                    desired != null && (
                      <ReplicaHealth running={svc.RunningTasks} desired={desired} />
                    ),
                    svc.UpdateStatus?.State,
                  ].filter(Boolean) as React.ReactNode[]
                }
              />
            );
          })}
        </div>
      )}
    </div>
  );
}
