import { api, headAllowedMethods } from "../api/client";
import type {
  ContainerConfig,
  Healthcheck,
  Integration,
  PortConfig,
  Service,
  ServiceDetail,
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
import { integrationLabelPrefix } from "../lib/integrationLabels";
import { cpuThresholds, memoryThresholds } from "../lib/resourceThresholds";
import { escapePromQL } from "../lib/utils";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

// Stable empties, so an absent list does not remount every consumer.
const emptyChanges: SpecChange[] = [];
const emptyIntegrations: Integration[] = [];

/**
 * A request the page replaced, or navigated away from, is not a failure.
 */
function ignoreUnlessAborted(signal: AbortSignal) {
  return (error: unknown) => {
    if (!signal.aborted) {
      console.warn(error);
    }
  };
}

export function useServiceDetail(id: string | undefined) {
  const queryClient = useQueryClient();
  const [tasks, setTasks] = useState<Task[]>([]);
  const [envVars, setEnvVars] = useState<Record<string, string> | null>(null);
  const [serviceResources, setServiceResources] = useState<ServiceResourceShape | null>(null);
  const [serviceLabels, setServiceLabels] = useState<Record<string, string> | null>(null);
  const [healthcheck, setHealthcheck] = useState<Healthcheck | null | undefined>(undefined);
  const [specPorts, setSpecPorts] = useState<PortConfig[] | null>(null);
  const [serviceMounts, setServiceMounts] = useState<ServiceMount[] | null>(null);
  const [containerConfig, setContainerConfig] = useState<ContainerConfig | null>(null);
  const monitoring = useMonitoringStatus();
  const [canChangeEndpointMode, setCanChangeEndpointMode] = useState(false);
  const hasPrometheus = isPrometheusReady(monitoring);
  const hasCadvisor = isCadvisorReady(monitoring);
  const [networkNames, setNetworkNames] = useState<Record<string, string>>({});
  const [cpuActual, setCpuActual] = useState<number | undefined>();
  const [memActual, setMemActual] = useState<number | undefined>();
  const { items: recommendations } = useRecommendations();

  const sseAbortRef = useRef<AbortController | null>(null);

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
  const fetchTasks = useCallback(
    (signal: AbortSignal) => {
      if (!id) {
        return;
      }

      api.serviceTasks(id, signal).then(setTasks).catch(ignoreUnlessAborted(signal));
    },
    [id],
  );

  // The per-service stream also delivers task events for this service's tasks,
  // so only treat `resource` as a Service when the event is a service event —
  // otherwise the page's own resource would be replaced with a Task.
  const onEvent = useCallback(
    (event: SSEEvent, { setData, refetch }: DetailResourceActions<ServiceDetail>) => {
      sseAbortRef.current?.abort();
      const controller = new AbortController();
      sseAbortRef.current = controller;
      fetchTasks(controller.signal);

      if (event.type === "service" && event.resource) {
        setData((previous) => ({ ...previous, service: event.resource as Service }));
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
    [fetchTasks, id, queryClient],
  );

  const {
    data: detail,
    history,
    error,
    retry: refetchService,
    allowedMethods,
  } = useDetailResource<ServiceDetail>(id, api.service, "/services", { onEvent });

  const service = detail?.service ?? null;
  const changes = detail?.changes ?? emptyChanges;
  const integrations = detail?.integrations ?? emptyIntegrations;

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
      .networks()
      .then(({ data: result }) => {
        const map: Record<string, string> = {};

        for (const network of result.items) {
          map[network.Id] = network.Name;
        }
        setNetworkNames(map);
      })
      .catch(console.warn);
  }, []);

  useEffect(() => {
    if (!id) {
      return;
    }

    headAllowedMethods(`/services/${id}/endpoint-mode`)
      .then((methods) => {
        setCanChangeEndpointMode(methods.has("PUT"));
      })
      .catch(() => {});
  }, [id]);

  useEffect(() => {
    if (!id) {
      return;
    }

    const controller = new AbortController();
    fetchTasks(controller.signal);

    return () => controller.abort();
  }, [id, fetchTasks]);

  useEffect(() => {
    return () => sseAbortRef.current?.abort();
  }, []);

  const serviceName = service?.Spec.Name || "";
  const taskMetrics = useTaskMetrics(
    serviceName
      ? `container_label_com_docker_swarm_service_name="${escapePromQL(serviceName)}"`
      : "",
    hasCadvisor && !!serviceName,
  );

  useEffect(() => {
    if (!serviceName || !hasCadvisor) {
      return;
    }

    let cancelled = false;
    const escaped = escapePromQL(serviceName);

    Promise.all([
      api.metricsQuery(
        `sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${escaped}"}[5m])) * 100`,
      ),
      api.metricsQuery(
        `sum(container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${escaped}"})`,
      ),
    ])
      .then(([cpuResp, memResp]) => {
        if (cancelled) {
          return;
        }

        const cpuVal = cpuResp.data?.result?.[0]?.value?.[1];
        const memVal = memResp.data?.result?.[0]?.value?.[1];

        if (cpuVal != null) {
          setCpuActual(Number(cpuVal));
        }

        if (memVal != null) {
          setMemActual(Number(memVal));
        }
      })
      .catch(console.warn);

    return () => {
      cancelled = true;
    };
  }, [serviceName, hasCadvisor]);

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
              query: `sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${escapePromQL(
                name,
              )}"}[5m])) * 100`,
              unit: "%",
              thresholds: cpuThresholds(service, thresholdReplicas),
              yMin: 0,
            },
            {
              title: "Memory Usage",
              query: `sum(container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${escapePromQL(name)}"})`,
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

    const prefixes = integrations
      .map(
        ({ name: integrationName }) =>
          integrationLabelPrefix[integrationName as keyof typeof integrationLabelPrefix],
      )
      .filter(Boolean);

    if (prefixes.length === 0) {
      return serviceLabels;
    }

    return Object.fromEntries(
      Object.entries(serviceLabels).filter(
        ([key]) => !prefixes.some((prefix) => key.startsWith(prefix)),
      ),
    );
  }, [serviceLabels, integrations]);

  const canPatch = allowedMethods.has("PATCH");

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
    allowedMethods,
    canPatch,
    canChangeEndpointMode,
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
