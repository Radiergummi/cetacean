import { api } from "../api/client";
import type {
  ContainerConfig,
  Healthcheck,
  Integration,
  PortConfig,
  Service,
  ServiceDetail,
  ServiceLink,
  ServiceMount,
  SpecChange,
  Task,
} from "../api/types";
import { composeQueryKey } from "../components/ComposeSection";
import type { ServiceResourceShape } from "../components/service-detail";
import type { DetailResourceActions } from "../hooks/useDetailResource";
import { useDetailResource } from "../hooks/useDetailResource";
import {
  isCadvisorReady,
  isPrometheusReady,
  useMonitoringStatus,
} from "../hooks/useMonitoringStatus";
import { useRecommendations } from "../hooks/useRecommendations";
import type { SSEEvent } from "../hooks/useResourceStream";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { getSemanticChartColor } from "../lib/chartColors";
import { deriveServiceSubResources } from "../lib/deriveServiceState";
import { cpuThresholds, memoryThresholds } from "../lib/resourceThresholds";
import { escapePromQL } from "../lib/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState } from "react";

// Stable empties, so an absent list does not remount every consumer.
const emptyChanges: SpecChange[] = [];
const emptyIntegrations: Integration[] = [];
const emptyLinks: ServiceLink[] = [];
const emptyTasks: Task[] = [];

/**
 * PromQL summing a service's usage across all of its running containers.
 */
function serviceUsageQueries(serviceName: string) {
  const selector = `container_label_com_docker_swarm_service_name="${escapePromQL(serviceName)}"`;

  return {
    cpu: `sum(rate(container_cpu_usage_seconds_total{${selector}}[5m])) * 100`,
    memory: `sum(container_memory_usage_bytes{${selector}})`,
  };
}

function sameLabels(
  left: Record<string, string> | null | undefined,
  right: Record<string, string> | null | undefined,
): boolean {
  const entries = Object.entries(left ?? {});

  return (
    entries.length === Object.keys(right ?? {}).length &&
    entries.every(([key, value]) => right?.[key] === value)
  );
}

export function useServiceDetail(id: string | undefined) {
  const queryClient = useQueryClient();
  const [envVars, setEnvVars] = useState<Record<string, string> | null>(null);
  const [serviceResources, setServiceResources] = useState<ServiceResourceShape | null>(null);
  const [serviceLabels, setServiceLabels] = useState<Record<string, string> | null>(null);
  const [healthcheck, setHealthcheck] = useState<Healthcheck | null | undefined>(undefined);
  const [specPorts, setSpecPorts] = useState<PortConfig[] | null>(null);
  const [serviceMounts, setServiceMounts] = useState<ServiceMount[] | null>(null);
  const [containerConfig, setContainerConfig] = useState<ContainerConfig | null>(null);
  const monitoring = useMonitoringStatus();
  const hasPrometheus = isPrometheusReady(monitoring);
  const hasCadvisor = isCadvisorReady(monitoring);
  const [networkNames, setNetworkNames] = useState<Record<string, string>>({});
  const { items: recommendations } = useRecommendations();

  const applyDerivedState = useCallback((service: Service) => {
    const {
      envVars,
      serviceResources,
      serviceLabels,
      healthcheck,
      specPorts,
      serviceMounts,
      containerConfig,
    } = deriveServiceSubResources(service);
    setEnvVars(envVars);
    setServiceResources(serviceResources);
    setServiceLabels(serviceLabels);
    setHealthcheck(healthcheck);
    setSpecPorts(specPorts);
    setServiceMounts(serviceMounts);
    setContainerConfig(containerConfig);
  }, []);

  // Tasks are the one side request this page still owns: they are a
  // collection of their own rather than part of the detail response.
  const tasksKey = useMemo(() => ["service-tasks", id] as const, [id]);
  const extraQueryKeys = useMemo(() => [tasksKey], [tasksKey]);
  const tasksQuery = useQuery({
    queryKey: tasksKey,
    queryFn: ({ signal }) => api.serviceTasks(id!, signal),
    enabled: !!id,
  });
  const tasks = tasksQuery.data ?? emptyTasks;

  // The per-service stream also delivers task events for this service's tasks,
  // so only treat `resource` as a Service when the event is a service event —
  // otherwise the page's own resource would be replaced with a Task.
  const onEvent = useCallback(
    (event: SSEEvent, { setData, refetch }: DetailResourceActions<ServiceDetail>) => {
      if (event.type === "service" && event.resource) {
        const service = event.resource as Service;
        let labelsChanged = false;

        setData((previous) => {
          labelsChanged = !sameLabels(previous.service.Spec.Labels, service.Spec.Labels);

          return { ...previous, service };
        });

        // The integrations and links are read from the labels on the server.
        if (labelsChanged) {
          refetch();
        }
      } else if (event.type === "service" || event.type === "sync") {
        // Service deletions and full syncs come without a payload — refetch to
        // pick up the new state (or surface 404 on delete).
        refetch();
      }

      // The compose document is a projection of the spec, so a task event does
      // not change it; an expanded section would otherwise keep showing the
      // file the service was exported as before the update.
      if (event.type === "service" || event.type === "sync") {
        void queryClient.invalidateQueries({ queryKey: [...composeQueryKey(`service:${id}`)] });
      }
    },
    [id, queryClient],
  );

  const {
    data: detail,
    history,
    error,
    retry: refetchService,
    allowedMethods,
    editable,
  } = useDetailResource<ServiceDetail>(id, api.service, "/services", {
    onEvent,
    extraQueryKeys,
  });

  const service = detail?.service ?? null;
  const changes = detail?.changes ?? emptyChanges;
  const integrations = detail?.integrations ?? emptyIntegrations;
  const links = detail?.links ?? emptyLinks;
  const imageUpdate = detail?.imageUpdate;

  // A recommendation names its target by ID, so a name-addressed page matches
  // none of them until the fetch has answered.
  const serviceRecommendations = useMemo(
    () => recommendations.filter(({ targetId }) => targetId === service?.ID),
    [recommendations, service?.ID],
  );

  // The sub-resource editors write their own results back, so these cannot be
  // derived on the fly — the fetch seeds them and a save replaces them.
  // Seeding during render rather than in an effect is React's own answer to
  // state that follows a value: an effect would show one frame of the previous
  // service's sub-resources first.
  const [derivedFrom, setDerivedFrom] = useState<Service | null>(null);

  if (service && service !== derivedFrom) {
    setDerivedFrom(service);
    applyDerivedState(service);
  }

  useEffect(() => {
    api
      .allNetworks()
      .then((networks) => {
        const map: Record<string, string> = {};

        for (const network of networks) {
          map[network.Id] = network.Name;
        }
        setNetworkNames(map);
      })
      .catch(console.warn);
  }, []);

  const serviceName = service?.Spec.Name || "";
  const taskMetrics = useTaskMetrics(
    serviceName
      ? `container_label_com_docker_swarm_service_name="${escapePromQL(serviceName)}"`
      : "",
    hasCadvisor && !!serviceName,
  );

  const usageQuery = useQuery({
    queryKey: ["service-usage", serviceName],
    queryFn: async () => {
      const queries = serviceUsageQueries(serviceName);
      const [cpuResponse, memoryResponse] = await Promise.all([
        api.metricsQuery(queries.cpu),
        api.metricsQuery(queries.memory),
      ]);
      const cpu = cpuResponse.data?.result?.[0]?.value?.[1];
      const memory = memoryResponse.data?.result?.[0]?.value?.[1];

      return {
        cpu: cpu != null ? Number(cpu) : undefined,
        memory: memory != null ? Number(memory) : undefined,
      };
    },
    enabled: !!serviceName && hasCadvisor,
  });
  const cpuActual = usageQuery.data?.cpu;
  const memActual = usageQuery.data?.memory;

  const name = service?.Spec.Name || service?.ID || "";

  // The chart queries sum across all running containers of the service, so the
  // matching threshold lines must scale with the same number of containers.
  // For replicated services that's the configured replica count; for global
  // services it's the count of running tasks (one per eligible node).
  const runningTasks = useMemo(
    () => tasks.filter(({ Status }) => Status?.State === "running").length,
    [tasks],
  );
  const thresholdReplicas = service?.Spec.Mode.Replicated?.Replicas ?? runningTasks;

  const metricsCharts = useMemo(
    () =>
      service
        ? [
            {
              title: "CPU Usage",
              query: serviceUsageQueries(name).cpu,
              unit: "%",
              thresholds: cpuThresholds(service, thresholdReplicas),
              yMin: 0,
            },
            {
              title: "Memory Usage",
              query: serviceUsageQueries(name).memory,
              unit: "bytes",
              thresholds: memoryThresholds(service, thresholdReplicas),
              yMin: 0,
              color: getSemanticChartColor("memory"),
            },
          ]
        : [],
    [name, service, thresholdReplicas],
  );

  const filteredLabels = useMemo(() => {
    if (!serviceLabels || integrations.length === 0) {
      return serviceLabels;
    }

    const claimed = new Set(integrations.flatMap(({ labels }) => labels));

    return Object.fromEntries(Object.entries(serviceLabels).filter(([key]) => !claimed.has(key)));
  }, [serviceLabels, integrations]);

  return {
    service,
    changes,
    tasks,
    history,
    envVars,
    onEnvSaved: setEnvVars,
    serviceResources,
    onResourcesSaved: setServiceResources,
    serviceLabels,
    onLabelsSaved: setServiceLabels,
    healthcheck,
    onHealthcheckSaved: setHealthcheck,
    specPorts,
    onPortsSaved: setSpecPorts,
    serviceMounts,
    onMountsSaved: setServiceMounts,
    containerConfig,
    onContainerConfigSaved: setContainerConfig,
    integrations,
    links,
    imageUpdate,
    allowedMethods,
    editable,
    hasPrometheus,
    hasCadvisor,
    error,
    networkNames,
    cpuActual,
    memActual,
    serviceRecommendations,
    name,
    metricsCharts,
    filteredLabels,
    taskMetrics,
    refetchService,
  };
}
