import { api } from "../api/client";
import type { Recommendation, Service, ServiceListItem } from "../api/types";
import DataTable, { type Column } from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import { HealthDot, ReplicaHealth } from "../components/HealthIndicator";
import ListToolbar from "../components/ListToolbar";
import { SkeletonTable } from "../components/LoadingSkeleton";
import { MetricsPanel, TaskSparkline } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import ResourceCard from "../components/ResourceCard";
import ResourceName from "../components/ResourceName";
import { SizingBadge } from "../components/SizingBadge";
import { useListPage } from "../hooks/useListPage";
import { isCadvisorReady, useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useRecommendations } from "../hooks/useRecommendations";
import { useServiceMetrics } from "../hooks/useServiceMetrics";
import { sizingCategories } from "../lib/sizingUtils";
import { sortColumn } from "../lib/sortColumn";
import { cardGridClass } from "../lib/styles";
import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";

export default function ServiceList() {
  const navigate = useNavigate();
  const {
    data: services,
    loading,
    error,
    retry,
    hasMore,
    loadMore,
    search,
    setSearch,
    sortKey,
    sortDir,
    toggle,
    viewMode,
    setViewMode,
  } = useListPage({
    path: "/services",
    sseType: "service",
    defaultSort: "name",
    viewModeKey: "services",
    fetchFn: (params, signal) => api.services(params, signal),
    keyFn: ({ ID }: ServiceListItem) => ID,
  });

  const monitoring = useMonitoringStatus();
  const hasCadvisor = isCadvisorReady(monitoring);
  const { getForService } = useServiceMetrics();
  const { items: recommendations, hasData: hasRecommendations } = useRecommendations();

  const recommendationsByService = useMemo(() => {
    const map = new Map<string, Recommendation[]>();

    for (const recommendation of recommendations) {
      if (recommendation.scope !== "service" || !sizingCategories.has(recommendation.category)) {
        continue;
      }

      const existing = map.get(recommendation.targetId);

      if (existing) {
        existing.push(recommendation);
      } else {
        map.set(recommendation.targetId, [recommendation]);
      }
    }

    return map;
  }, [recommendations]);

  const baseColumns: Column<ServiceListItem>[] = useMemo(
    () => [
      {
        ...sortColumn("Name", "name", sortKey, sortDir, toggle),
        cell: ({ ID, Spec }) => (
          <Link
            to={`/services/${ID}`}
            className="font-medium text-link hover:underline"
            onClick={(event) => event.stopPropagation()}
          >
            <ResourceName name={Spec.Name} />
          </Link>
        ),
      },
      {
        header: "Image",
        cell: ({ Spec }) => (
          <span className="font-mono text-xs">
            {Spec.TaskTemplate.ContainerSpec?.Image?.split("@")[0]}
          </span>
        ),
      },
      {
        ...sortColumn("Mode", "mode", sortKey, sortDir, toggle),
        cell: ({ Spec }) => (Spec.Mode.Replicated ? "replicated" : "global"),
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
              {ports
                .map(({ Protocol, PublishedPort }) => `${PublishedPort}/${Protocol}`)
                .join(", ")}
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
    ],
    [sortKey, sortDir, toggle],
  );

  const metricsColumns: Column<ServiceListItem>[] = useMemo(
    () =>
      hasCadvisor
        ? [
            {
              header: "CPU (1h)",
              cell: ({ Spec }) => {
                const metrics = getForService(Spec.Name);
                return (
                  <TaskSparkline
                    data={metrics.cpuHistory}
                    currentValue={metrics.cpu}
                    type="cpu"
                  />
                );
              },
            },
            {
              header: "Memory (1h)",
              cell: ({ Spec }) => {
                const metrics = getForService(Spec.Name);
                return (
                  <TaskSparkline
                    data={metrics.memoryHistory}
                    currentValue={metrics.memory}
                    type="memory"
                  />
                );
              },
            },
          ]
        : [],
    [hasCadvisor, getForService],
  );

  const sizingColumns: Column<ServiceListItem>[] = useMemo(
    () =>
      hasRecommendations
        ? [
            {
              header: "Sizing",
              cell: ({ ID }) => {
                const hints = recommendationsByService.get(ID) ?? [];
                return <SizingBadge hints={hints} />;
              },
            },
          ]
        : [],
    [hasRecommendations, recommendationsByService],
  );

  const columns: Column<ServiceListItem>[] = useMemo(
    () => [...baseColumns, ...metricsColumns, ...sizingColumns],
    [baseColumns, metricsColumns, sizingColumns],
  );

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
        placeholder="Search services…"
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
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      ) : (
        <div className={cardGridClass}>
          {services.map((service) => {
            const desired = service.Spec.Mode.Replicated?.Replicas;

            return (
              <ResourceCard
                key={service.ID}
                title={
                  <span className="flex items-center gap-2">
                    <HealthDot
                      running={service.RunningTasks}
                      desired={desired ?? service.RunningTasks}
                    />
                    <ResourceName name={service.Spec.Name} />
                  </span>
                }
                to={`/services/${service.ID}`}
                badge={<ServiceStatusBadge service={service} />}
                subtitle={service.Spec.TaskTemplate.ContainerSpec?.Image?.split("@")[0]}
                meta={[
                  desired != null ? (
                    <ReplicaHealth
                      key="replicas"
                      running={service.RunningTasks}
                      desired={desired}
                    />
                  ) : (
                    <span
                      key="mode"
                      className="rounded-md bg-muted px-1.5 py-0.5 text-xs text-muted-foreground"
                    >
                      global
                    </span>
                  ),
                ]}
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
