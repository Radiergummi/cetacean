import type React from "react";
import { useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSortParams } from "../hooks/useSort";
import { useViewMode } from "../hooks/useViewMode";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Service, ServiceListItem } from "../api/types";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import ListToolbar from "../components/ListToolbar";
import PageHeader from "../components/PageHeader";
import DataTable, { type Column } from "../components/DataTable";
import SortIndicator from "../components/SortIndicator";
import ResourceCard from "../components/ResourceCard";
import EmptyState from "../components/EmptyState";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import { MetricsPanel } from "../components/metrics";
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
  const monitoring = useMonitoringStatus();
  const hasCadvisor =
    monitoring?.prometheusConfigured &&
    monitoring?.prometheusReachable &&
    !!monitoring?.cadvisor?.targets;

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
      header: "Ports",
      cell: (svc) => {
        const ports = svc.Endpoint?.Ports;
        if (!ports || ports.length === 0) return null;
        return (
          <span className="font-mono text-xs text-muted-foreground">
            {ports.map((p) => `${p.PublishedPort}/${p.Protocol}`).join(", ")}
          </span>
        );
      },
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
      header: "Status",
      cell: (svc) => <ServiceStatusBadge service={svc} />,
    },
  ];

  if (loading)
    return (
      <div>
        <PageHeader title="Services" />
        <SkeletonTable columns={6} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Services" />
      {hasCadvisor && (
        <div className="mb-6">
          <ErrorBoundary inline>
            <MetricsPanel
              header="Resource Usage by Service"
              charts={[
                {
                  title: "CPU Usage (top 10)",
                  query: `topk(10, sum by (container_label_com_docker_swarm_service_name)(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name!=""}[5m])) * 100)`,
                  unit: "%",
                  yMin: 0,
                },
                {
                  title: "Memory Usage (top 10)",
                  query: `topk(10, sum by (container_label_com_docker_swarm_service_name)(container_memory_usage_bytes{container_label_com_docker_swarm_service_name!=""}))`,
                  unit: "bytes",
                  yMin: 0,
                },
              ]}
            />
          </ErrorBoundary>
        </div>
      )}
      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search services..."
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />
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
                    svc.Endpoint?.Ports &&
                      svc.Endpoint.Ports.length > 0 && (
                        <span className="font-mono text-xs">
                          {svc.Endpoint.Ports.map((p) => `${p.PublishedPort}/${p.Protocol}`).join(
                            ", ",
                          )}
                        </span>
                      ),
                    <ServiceStatusBadge service={svc} />,
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

const statusLabels: Record<string, string> = {
  updating: "Updating",
  completed: "Stable",
  paused: "Paused",
  rollback_started: "Rolling back",
  rollback_paused: "Rollback paused",
  rollback_completed: "Rolled back",
};

const statusColors: Record<string, string> = {
  stable: "text-green-600 dark:text-green-400",
  updating: "text-blue-600 dark:text-blue-400",
  rollback_started: "text-amber-600 dark:text-amber-400",
  paused: "text-amber-600 dark:text-amber-400",
  rollback_paused: "text-amber-600 dark:text-amber-400",
  rollback_completed: "text-amber-600 dark:text-amber-400",
};

function ServiceStatusBadge({ service }: { service: Pick<Service, "UpdateStatus"> }) {
  const state = service.UpdateStatus?.State;
  const label = !state || state === "completed" ? "Stable" : statusLabels[state] || state;
  const color = statusColors[state || "stable"] || statusColors.stable;

  return <span className={`text-sm font-medium ${color}`}>{label}</span>;
}
