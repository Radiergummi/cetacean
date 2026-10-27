import { api } from "../api/client";
import type { Recommendation, Service, ServiceListItem } from "../api/types";
import { NewerImageBadge } from "../components/data";
import type { Column } from "../components/DataTable";
import ErrorBoundary from "../components/ErrorBoundary";
import { HealthDot, ReplicaHealth } from "../components/HealthIndicator";
import { MetricsPanel, TaskSparkline } from "../components/metrics";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import { SizingBadge } from "../components/SizingBadge";
import { useJobOutcomes } from "../hooks/useJobOutcomes";
import { isCadvisorReady, useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useRecommendations } from "../hooks/useRecommendations";
import { useServiceMetrics } from "../hooks/useServiceMetrics";
import { rolloutToneClass, serviceModeLabel, serviceUpdateStatus } from "../lib/deriveServiceState";
import { desiredTasks } from "../lib/serviceHealth";
import { sizingCategories } from "../lib/sizingUtils";
import { sortColumn } from "../lib/sortColumn";
import { cn } from "../lib/utils";
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
          cell: ({ Spec, ImageUpdate }) => (
            <span className="inline-flex items-center gap-2">
              <span className="font-mono text-xs">
                {Spec.TaskTemplate?.ContainerSpec?.Image?.split("@")[0]}
              </span>
              {ImageUpdate && <NewerImageBadge update={ImageUpdate} />}
            </span>
          ),
        },
        {
          ...sortColumn("Mode", "mode", sortKey, sortDir, toggle),
          cell: ({ Spec }) => serviceModeLabel(Spec.Mode),
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
          header: "Rollout",
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
                <ServiceHealthDot service={service} />
                <ResourceName name={service.Spec.Name} />
              </span>
            }
            to={`/services/${service.ID}`}
            badge={
              <span className="inline-flex items-center gap-2">
                {service.ImageUpdate && <NewerImageBadge update={service.ImageUpdate} />}
                <ServiceStatusBadge service={service} />
              </span>
            }
            subtitle={service.Spec.TaskTemplate?.ContainerSpec?.Image?.split("@")[0]}
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
                  {serviceModeLabel(service.Spec.Mode)}
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

/** A card's health dot; a job's asks how its finished tasks ended. */
function ServiceHealthDot({ service }: { service: ServiceListItem }) {
  const services = useMemo(() => [service], [service]);
  const outcomes = useJobOutcomes(services);

  return (
    <HealthDot
      running={service.RunningTasks}
      desired={desiredTasks(service.Spec.Mode)}
      outcome={outcomes[service.ID]}
    />
  );
}

/** A service's rollout state — Docker's `UpdateStatus`, and nothing about health. */
function ServiceStatusBadge({ service }: { service: Pick<Service, "UpdateStatus"> }) {
  const { label, state } = serviceUpdateStatus(service);

  return <span className={cn("text-sm font-medium", rolloutToneClass(state))}>{label}</span>;
}
