import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSort } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Service } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import SortableHeader from "../components/SortableHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";

const sortAccessors = {
  name: (s: Service) => s.Spec.Name,
  image: (s: Service) => s.Spec.TaskTemplate.ContainerSpec.Image.split("@")[0],
  mode: (s: Service) => (s.Spec.Mode.Replicated ? "replicated" : "global"),
  replicas: (s: Service) => s.Spec.Mode.Replicated?.Replicas ?? 0,
  update: (s: Service) => s.UpdateStatus?.State ?? "",
};

export default function ServiceList() {
  const {
    data: services,
    loading,
    error,
    retry,
  } = useSwarmResource(api.services, "service", (s: Service) => s.ID);
  const [search, setSearch] = useSearchParam("q");
  const [viewMode, setViewMode] = useViewMode("services");
  const navigate = useNavigate();
  const filtered = useMemo(
    () => services.filter((s) => s.Spec.Name.toLowerCase().includes(search.toLowerCase())),
    [services, search],
  );
  const { sorted, sortKey, sortDir, toggle } = useSort(filtered, sortAccessors, "name");

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
      {sorted.length === 0 ? (
        <EmptyState message={search ? "No services match your search" : "No services found"} />
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
                  label="Image"
                  sortKey="image"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Mode"
                  sortKey="mode"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Replicas"
                  sortKey="replicas"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
                <SortableHeader
                  label="Update Status"
                  sortKey="update"
                  activeSortKey={sortKey}
                  sortDir={sortDir}
                  onToggle={toggle}
                />
              </tr>
            </thead>
            <tbody>
              {sorted.map((svc) => (
                <tr
                  key={svc.ID}
                  className="border-b cursor-pointer hover:bg-muted/50"
                  onClick={() => navigate(`/services/${svc.ID}`)}
                >
                  <td className="p-3">
                    <Link
                      to={`/services/${svc.ID}`}
                      className="text-link hover:underline font-medium"
                      onClick={(e) => e.stopPropagation()}
                    >
                      {svc.Spec.Name}
                    </Link>
                  </td>
                  <td className="p-3 text-sm font-mono text-xs">
                    {svc.Spec.TaskTemplate.ContainerSpec.Image.split("@")[0]}
                  </td>
                  <td className="p-3 text-sm">
                    {svc.Spec.Mode.Replicated ? "replicated" : "global"}
                  </td>
                  <td className="p-3 text-sm">{svc.Spec.Mode.Replicated?.Replicas ?? "\u2014"}</td>
                  <td className="p-3 text-sm">{svc.UpdateStatus?.State || "\u2014"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {sorted.map((svc) => (
            <Link
              key={svc.ID}
              to={`/services/${svc.ID}`}
              className="rounded-lg border bg-card p-4 hover:border-foreground/20 hover:shadow-sm transition-all"
            >
              <div className="font-medium mb-2 truncate">{svc.Spec.Name}</div>
              <div className="text-xs font-mono text-muted-foreground truncate mb-3">
                {svc.Spec.TaskTemplate.ContainerSpec.Image.split("@")[0]}
              </div>
              <div className="flex items-center gap-3 text-xs text-muted-foreground">
                <span>{svc.Spec.Mode.Replicated ? "replicated" : "global"}</span>
                {svc.Spec.Mode.Replicated && (
                  <span className="tabular-nums">{svc.Spec.Mode.Replicated.Replicas} replicas</span>
                )}
                {svc.UpdateStatus?.State && <span>{svc.UpdateStatus.State}</span>}
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
