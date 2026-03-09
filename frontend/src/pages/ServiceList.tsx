import { useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSortParams } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Service } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import DataTable, { type Column } from "../components/DataTable";
import SortIndicator from "../components/SortIndicator";
import ViewToggle from "../components/ViewToggle";
import ResourceCard from "../components/ResourceCard";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";

export default function ServiceList() {
  const [search, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("name");
  const {
    data: services,
    loading,
    error,
    retry,
  } = useSwarmResource(
    useCallback(
      () => api.services({ search, sort: sortKey, dir: sortDir }),
      [search, sortKey, sortDir],
    ),
    "service",
    (s: Service) => s.ID,
  );
  const [viewMode, setViewMode] = useViewMode("services");
  const navigate = useNavigate();

  const columns: Column<Service>[] = [
    {
      header: <SortIndicator label="Name" active={sortKey === "name"} dir={sortDir} />,
      cell: (svc) => (
        <Link
          to={`/services/${svc.ID}`}
          className="text-link hover:underline font-medium"
          onClick={(e) => e.stopPropagation()}
        >
          {svc.Spec.Name}
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
      cell: (svc) => svc.Spec.Mode.Replicated?.Replicas ?? "\u2014",
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
          {services.map((svc) => (
            <ResourceCard
              key={svc.ID}
              title={svc.Spec.Name}
              to={`/services/${svc.ID}`}
              subtitle={svc.Spec.TaskTemplate.ContainerSpec.Image.split("@")[0]}
              meta={[
                svc.Spec.Mode.Replicated ? "replicated" : "global",
                svc.Spec.Mode.Replicated && <span className="tabular-nums">{svc.Spec.Mode.Replicated.Replicas} replicas</span>,
                svc.UpdateStatus?.State,
              ].filter(Boolean) as React.ReactNode[]}
            />
          ))}
        </div>
      )}
    </div>
  );
}
