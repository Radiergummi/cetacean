import { api } from "../api/client";
import type { Service, ServiceListItem } from "../api/types";
import DataTable, { type Column } from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import { SkeletonTable } from "../components/LoadingSkeleton";
import { MetricsPanel } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import ResourceCard from "../components/ResourceCard";
import ResourceName from "../components/ResourceName";
import SortIndicator from "../components/SortIndicator";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useSearchParam } from "../hooks/useSearchParam";
import { useSortParams } from "../hooks/useSort";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import type React from "react";
import { useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";

function ReplicaHealth({ running, desired }: { running: number; desired: number }) {
  const healthy = running >= desired && desired > 0;

  return (
    <span
      data-healthy={healthy || undefined}
      className="font-medium text-red-600 tabular-nums data-healthy:text-green-600 dark:text-red-400 dark:data-healthy:text-green-400"
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
    ({ ID }: ServiceListItem) => ID,
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
      header: (
        <SortIndicator
          label="Name"
          active={sortKey === "name"}
          dir={sortDir}
        />
      ),
      cell: ({ ID, Spec }) => (
        <Link
          to={`/services/${ID}`}
          className="font-medium text-link hover:underline"
          onClick={(event) => event.stopPropagation()}
        >
          <ResourceName name={Spec.Name} />
        </Link>
      ),
      onHeaderClick: () => toggle("name"),
    },
    {
      header: "Image",
      cell: ({ Spec }) => (
        <span className="font-mono text-xs">
          {Spec.TaskTemplate.ContainerSpec.Image.split("@")[0]}
        </span>
      ),
    },
    {
      header: (
        <SortIndicator
          label="Mode"
          active={sortKey === "mode"}
          dir={sortDir}
        />
      ),
      cell: ({ Spec }) => (Spec.Mode.Replicated ? "replicated" : "global"),
      onHeaderClick: () => toggle("mode"),
    },
    {
      header: "Ports",
      cell: ({ Endpoint }) => {
        const ports = Endpoint?.Ports;

        if (!ports || ports.length === 0) {
          return null;
        }

        return (
          <span className="font-mono text-xs text-muted-foreground">
            {ports.map(({ Protocol, PublishedPort }) => `${PublishedPort}/${Protocol}`).join(", ")}
          </span>
        );
      },
    },
    {
      header: "Replicas",
      cell: ({ RunningTasks, Spec }) => {
        const desired = Spec.Mode.Replicated?.Replicas;

        if (desired == null) {
          return "\u2014";
        }

        return (
          <ReplicaHealth
            running={RunningTasks}
            desired={desired}
          />
        );
      },
    },
    {
      header: "Status",
      cell: (service) => <ServiceStatusBadge service={service} />,
    },
  ];

  if (loading) {
    return (
      <div>
        <PageHeader title="Services" />
        <SkeletonTable columns={6} />
      </div>
    );
  }

  if (error) {
    return (
      <FetchError
        message={error.message}
        onRetry={retry}
      />
    );
  }

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
          keyFn={({ ID }) => ID}
          onRowClick={({ ID }) => navigate(`/services/${ID}`)}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {services.map((service) => {
            const desired = service.Spec.Mode.Replicated?.Replicas;

            return (
              <ResourceCard
                key={service.ID}
                title={<ResourceName name={service.Spec.Name} />}
                to={`/services/${service.ID}`}
                subtitle={service.Spec.TaskTemplate.ContainerSpec.Image.split("@")[0]}
                meta={
                  [
                    service.Spec.Mode.Replicated ? "replicated" : "global",
                    desired != null && (
                      <ReplicaHealth
                        key="replicas"
                        running={service.RunningTasks}
                        desired={desired}
                      />
                    ),
                    service.Endpoint?.Ports && service.Endpoint.Ports.length > 0 && (
                      <span
                        key="ports"
                        className="font-mono text-xs"
                      >
                        {service.Endpoint.Ports.map(
                          ({ Protocol, PublishedPort }) => `${PublishedPort}/${Protocol}`,
                        ).join(", ")}
                      </span>
                    ),
                    <ServiceStatusBadge
                      key="status"
                      service={service}
                    />,
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

function ServiceStatusBadge({ service }: { service: Pick<Service, "UpdateStatus"> }) {
  const raw = service.UpdateStatus?.State;
  const state = !raw || raw === "completed" ? "stable" : raw;
  const label = statusLabels[state] || state;

  return (
    <span
      data-state={state}
      className="text-sm font-medium text-green-600 data-[state=paused]:text-amber-600 data-[state=rollback_completed]:text-amber-600 data-[state=rollback_paused]:text-amber-600 data-[state=rollback_started]:text-amber-600 data-[state=updating]:text-blue-600 dark:text-green-400 dark:data-[state=paused]:text-amber-400 dark:data-[state=rollback_completed]:text-amber-400 dark:data-[state=rollback_paused]:text-amber-400 dark:data-[state=rollback_started]:text-amber-400 dark:data-[state=updating]:text-blue-400"
    >
      {label}
    </span>
  );
}
