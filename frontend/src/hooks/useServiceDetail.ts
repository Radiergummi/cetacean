import { api, emptyMethods, headAllowedMethods } from "../api/client";
import type {
  ContainerConfig,
  Healthcheck,
  HistoryEntry,
  Integration,
  PortConfig,
  Service,
  ServiceMount,
  SpecChange,
  Task,
} from "../api/types";
import type { ServiceResourceShape } from "../components/service-detail";
import {
  isCadvisorReady,
  isPrometheusReady,
  useMonitoringStatus,
} from "../hooks/useMonitoringStatus";
import { useRecommendations } from "../hooks/useRecommendations";
import { useResourceStream } from "../hooks/useResourceStream";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { getSemanticChartColor } from "../lib/chartColors";
import { deriveServiceSubResources } from "../lib/deriveServiceState";
import { integrationLabelPrefix } from "../lib/integrationLabels";
import { cpuThresholds, memoryThresholds } from "../lib/resourceThresholds";
import { escapePromQL } from "../lib/utils";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export function useServiceDetail(id: string | undefined) {
  const [service, setService] = useState<Service | null>(null);
  const [changes, setChanges] = useState<SpecChange[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [envVars, setEnvVars] = useState<Record<string, string> | null>(null);
  const [serviceResources, setServiceResources] = useState<ServiceResourceShape | null>(null);
  const [serviceLabels, setServiceLabels] = useState<Record<string, string> | null>(null);
  const [healthcheck, setHealthcheck] = useState<Healthcheck | null | undefined>(undefined);
  const [specPorts, setSpecPorts] = useState<PortConfig[] | null>(null);
  const [serviceMounts, setServiceMounts] = useState<ServiceMount[] | null>(null);
  const [containerConfig, setContainerConfig] = useState<ContainerConfig | null>(null);
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const monitoring = useMonitoringStatus();
  const [allowedMethods, setAllowedMethods] = useState(emptyMethods);
  const [canChangeEndpointMode, setCanChangeEndpointMode] = useState(false);
  const hasPrometheus = isPrometheusReady(monitoring);
  const hasCadvisor = isCadvisorReady(monitoring);
  const [error, setError] = useState(false);
  const [networkNames, setNetworkNames] = useState<Record<string, string>>({});
  const [cpuActual, setCpuActual] = useState<number | undefined>();
  const [memActual, setMemActual] = useState<number | undefined>();
  const { items: recommendations } = useRecommendations();
  const serviceRecommendations = useMemo(
    () => recommendations.filter(({ targetId }) => targetId === id),
    [recommendations, id],
  );

  const abortRef = useRef<AbortController | null>(null);
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

  const fetchService = useCallback(
    (signal: AbortSignal) => {
      if (!id) {
        return;
      }

      api
        .service(id, signal)
        .then(({ data: response, allowedMethods: methods }) => {
          setService(response.service);
          setChanges(response.changes ?? []);
          setIntegrations(response.integrations ?? []);
          applyDerivedState(response.service);
          setAllowedMethods(methods);
        })
        .catch(() => {
          if (!signal.aborted) {
            setError(true);
          }
        });
    },
    [id, applyDerivedState],
  );

  const fetchSideData = useCallback(
    (signal: AbortSignal) => {
      if (!id) {
        return;
      }

      const ignore = (error: unknown) => {
        if (!signal.aborted) {
          console.warn(error);
        }
      };

      api.serviceTasks(id, signal).then(setTasks).catch(ignore);
      api.history({ resourceId: id, limit: 10 }, signal).then(setHistory).catch(ignore);
    },
    [id],
  );

  const refetchService = useCallback(() => {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    fetchService(controller.signal);
  }, [fetchService]);

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

    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;

    fetchService(controller.signal);
    fetchSideData(controller.signal);

    return () => controller.abort();
  }, [id, fetchService, fetchSideData]);

  useResourceStream(`/services/${id}`, (event) => {
    if (!id) {
      return;
    }

    if (event.resource) {
      const svc = event.resource as Service;
      setService(svc);
      applyDerivedState(svc);
    }

    sseAbortRef.current?.abort();
    const controller = new AbortController();
    sseAbortRef.current = controller;
    fetchSideData(controller.signal);

    if (!event.resource) {
      fetchService(controller.signal);
    }
  });

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
              thresholds: cpuThresholds(service),
              yMin: 0,
            },
            {
              title: "Memory Usage",
              query: `sum(container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${escapePromQL(name)}"})`,
              unit: "bytes",
              thresholds: memoryThresholds(service),
              yMin: 0,
              color: getSemanticChartColor("memory"),
            },
          ]
        : [],
    [name, service],
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
