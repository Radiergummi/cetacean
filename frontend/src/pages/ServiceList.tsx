import { api } from "../api/client";
import type { Recommendation, Service, ServiceListItem } from "../api/types";
import type { Column } from "../components/DataTable";
import ErrorBoundary from "../components/ErrorBoundary";
import { HealthDot, ReplicaHealth } from "../components/HealthIndicator";
import { MetricsPanel, TaskSparkline } from "../components/metrics";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import { SizingBadge } from "../components/SizingBadge";
import { isCadvisorReady, useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useRecommendations } from "../hooks/useRecommendations";
import { useServiceMetrics } from "../hooks/useServiceMetrics";
import { serviceUpdateStatus } from "../lib/deriveServiceState";
import { sizingCategories } from "../lib/sizingUtils";
import { sortColumn } from "../lib/sortColumn";
import { useCallback, useMemo } from "react";
import { Link } from "react-router-dom";

export default function ServiceList() {
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

  const columns = useCallback(
    (sortKey: string | undefined, sortDir: "asc" | "desc", toggle: (key: string) => void) => {
      const base: Column<ServiceListItem>[] = [
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
      ];

      const metrics: Column<ServiceListItem>[] = hasCadvisor
        ? [
            {
              header: "CPU (1h)",
              cell: ({ Spec }) => {
                const serviceMetrics = getForService(Spec.Name);
                return (
                  <TaskSparkline
                    data={serviceMetrics.cpuHistory}
                    currentValue={serviceMetrics.cpu}
                    type="cpu"
                  />
                );
              },
            },
            {
              header: "Memory (1h)",
              cell: ({ Spec }) => {
                const serviceMetrics = getForService(Spec.Name);
                return (
                  <TaskSparkline
                    data={serviceMetrics.memoryHistory}
                    currentValue={serviceMetrics.memory}
                    type="memory"
                  />
                );
              },
            },
          ]
        : [];

      const sizing: Column<ServiceListItem>[] = hasRecommendations
        ? [
            {
              header: "Sizing",
              cell: ({ ID }) => {
                const hints = recommendationsByService.get(ID) ?? [];
                return <SizingBadge hints={hints} />;
              },
            },
          ]
        : [];

      return [...base, ...metrics, ...sizing];
    },
    [hasCadvisor, getForService, hasRecommendations, recommendationsByService],
  );

  return (
    <ResourceListPage<ServiceListItem>
      title="Services"
      path="/services"
      sseType="service"
      defaultSort="name"
      searchPlaceholder="Search services…"
      viewModeKey="services"
      fetchFn={(params, signal) => api.services(params, signal)}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `/services/${ID}`}
      columns={columns}
      skeletonColumns={6}
      headerContent={
        hasCadvisor ? (
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
        ) : undefined
      }
      renderCard={(service) => {
        const desired = service.Spec.Mode.Replicated?.Replicas;

        return (
          <ResourceCard
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
      }}
      emptyMessage={(hasSearch) =>
        hasSearch ? "No services match your search" : "No services found"
      }
    />
  );
}

function ServiceStatusBadge({ service }: { service: Pick<Service, "UpdateStatus"> }) {
  const { label, state } = serviceUpdateStatus(service);

  return (
    <span
      data-state={state}
      className="text-sm font-medium text-green-600 data-[state=paused]:text-amber-600 data-[state=rollback_completed]:text-amber-600 data-[state=rollback_paused]:text-amber-600 data-[state=rollback_started]:text-amber-600 data-[state=updating]:text-blue-600 dark:text-green-400 dark:data-[state=paused]:text-amber-400 dark:data-[state=rollback_completed]:text-amber-400 dark:data-[state=rollback_paused]:text-amber-400 dark:data-[state=rollback_started]:text-amber-400 dark:data-[state=updating]:text-blue-400"
    >
      {label}
    </span>
  );
}
