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
import ActivityFeed from "../components/ActivityFeed";
import CollapsibleSection from "../components/CollapsibleSection";
import { ContainerImage, KVTable, MetadataGrid, ResourceLink, Timestamp } from "../components/data";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { KeyValueEditor } from "../components/KeyValueEditor";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { LogViewer } from "../components/log";
import { MetricsPanel } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import {
  CapabilitiesEditor,
  CommandEditor,
  ConfigsEditor,
  DeploymentChanges,
  DnsEditor,
  EndpointModeEditor,
  EnvEditor,
  ExtraHostsEditor,
  HealthcheckEditor,
  LogDriverEditor,
  MountsEditor,
  NetworksEditor,
  PlacementEditor,
  PolicyEditor,
  PortsEditor,
  ReplicaCard,
  ResourcesEditor,
  RuntimeEditor,
  SecretsEditor,
  ServiceActions,
  type ServiceResourceShape,
} from "../components/service-detail";
import { CronjobPanel } from "../components/service-detail/CronjobPanel";
import { DiunPanel } from "../components/service-detail/DiunPanel";
import { ShepherdPanel } from "../components/service-detail/ShepherdPanel";
import { TraefikPanel } from "../components/service-detail/TraefikPanel";
import { SizingBanner } from "../components/SizingBanner";
import TasksTable from "../components/TasksTable";
import { Tooltip, TooltipContent, TooltipTrigger } from "../components/ui/tooltip";
import {
  isCadvisorReady,
  isPrometheusReady,
  useMonitoringStatus,
} from "../hooks/useMonitoringStatus";
import { useRecommendations } from "../hooks/useRecommendations";
import { useResourceStream } from "../hooks/useResourceStream";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { getSemanticChartColor } from "../lib/chartColors";
import { deriveServiceSubResources, serviceUpdateStatus } from "../lib/deriveServiceState";
import { formatDuration, formatRelativeDate } from "../lib/format";
import { integrationLabelPrefix, rawLabelsForIntegration } from "../lib/integrationLabels";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
import { cpuThresholds, memoryThresholds } from "../lib/resourceThresholds";
import { escapePromQL } from "../lib/utils";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";

export default function ServiceDetail() {
  const { id } = useParams<{ id: string }>();
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
  const serviceRecommendations = recommendations.filter((r) => r.targetId === id);

  const abortRef = useRef<AbortController | null>(null);
  const sseAbortRef = useRef<AbortController | null>(null);

  const applyDerivedState = useCallback((svc: Service) => {
    const derived = deriveServiceSubResources(svc);
    setEnvVars(derived.envVars);
    setServiceResources(derived.serviceResources);
    setServiceLabels(derived.serviceLabels);
    setHealthcheck(derived.healthcheck);
    setSpecPorts(derived.specPorts);
    setServiceMounts(derived.serviceMounts);
    setContainerConfig(derived.containerConfig);
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

    // Use SSE payload for optimistic service update
    if (event.resource) {
      const svc = event.resource as Service;
      setService(svc);
      applyDerivedState(svc);
    }

    // Refetch tasks and history — not in the service object.
    // Abort previous SSE-triggered fetches so they don't outlive an Unmount.
    sseAbortRef.current?.abort();
    const controller = new AbortController();
    sseAbortRef.current = controller;
    fetchSideData(controller.signal);

    // On sync events (no resource), also refetch service for fresh changes/diff
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

  if (error) {
    return <FetchError message="Failed to load service" />;
  }

  if (!service) {
    return <LoadingDetail />;
  }

  const taskTemplate = service.Spec.TaskTemplate;
  const containerSpec = taskTemplate?.ContainerSpec;
  const placement = taskTemplate?.Placement;
  const canPatch = allowedMethods.has("PATCH");
  const hasPlacementContent =
    (placement?.Constraints && placement.Constraints.length > 0) ||
    (placement?.Preferences && placement.Preferences.length > 0) ||
    (placement?.MaxReplicas != null && placement.MaxReplicas > 0);
  const hasHealthcheckContent =
    healthcheck != null && !(healthcheck.Test?.length === 1 && healthcheck.Test[0] === "NONE");
  const hasPortsContent = specPorts != null && specPorts.length > 0;
  const hasEnvContent = envVars != null && Object.keys(envVars).length > 0;
  const hasLabelsContent = filteredLabels != null && Object.keys(filteredLabels).length > 0;
  const hasResourcesContent =
    serviceResources != null &&
    (serviceResources.Limits?.NanoCPUs != null ||
      serviceResources.Limits?.MemoryBytes != null ||
      serviceResources.Reservations?.NanoCPUs != null ||
      serviceResources.Reservations?.MemoryBytes != null ||
      taskTemplate?.Resources?.Limits?.Pids != null);
  const labels = service.Spec.Labels;

  const runningTasks = tasks?.filter(({ Status }) => Status?.State === "running").length ?? 0;
  const resources = service?.Spec?.TaskTemplate?.Resources;
  const cpuReserved = resources?.Reservations?.NanoCPUs
    ? (resources.Reservations.NanoCPUs / 1e9) * 100 * runningTasks
    : undefined;
  const cpuLimit = resources?.Limits?.NanoCPUs
    ? (resources.Limits.NanoCPUs / 1e9) * 100 * runningTasks
    : undefined;
  const memReserved = resources?.Reservations?.MemoryBytes
    ? resources.Reservations.MemoryBytes * runningTasks
    : undefined;
  const memLimit = resources?.Limits?.MemoryBytes
    ? resources.Limits.MemoryBytes * runningTasks
    : undefined;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          <ResourceName
            name={name}
            direction="responsive"
          />
        }
        breadcrumbs={[
          { label: "Services", to: "/services" },
          { label: <ResourceName name={name} /> },
        ]}
        actions={
          <ServiceActions
            service={service}
            serviceId={id!}
            allowedMethods={allowedMethods}
          />
        }
      />

      <SizingBanner
        hints={serviceRecommendations}
        canFix={canPatch}
        onFixed={() => {
          if (id) {
            api
              .service(id)
              .then(({ data: response }) => {
                setService(response.service);
                applyDerivedState(response.service);
              })
              .catch(() => {
                // Refetch failed — page will refresh on next SSE event
              });
          }
        }}
      />

      {/* Overview cards */}
      <MetadataGrid>
        <ContainerImage
          image={containerSpec?.Image ?? ""}
          serviceId={id}
          canEdit={allowedMethods.has("PUT")}
        />
        <ReplicaCard
          service={service}
          tasks={tasks}
          allowedMethods={allowedMethods}
        />
        <ServiceStatusCard service={service} />
        <ResourceLink
          label="Stack"
          name={labels?.["com.docker.stack.namespace"]}
          to={`/stacks/${labels?.["com.docker.stack.namespace"]}`}
        />
        <Timestamp
          label="Created"
          date={service.CreatedAt}
        />
        <Timestamp
          label="Updated"
          date={service.UpdatedAt}
        />
      </MetadataGrid>

      {/* Tasks */}
      <TasksTable
        tasks={tasks}
        variant="service"
        metrics={hasCadvisor ? taskMetrics : undefined}
      />

      {hasPrometheus && (
        <ErrorBoundary inline>
          <MetricsPanel
            header="Metrics"
            charts={metricsCharts}
          />
        </ErrorBoundary>
      )}

      {(changes.length > 0 || history.length > 0) && (
        <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
          {changes.length > 0 && (
            <CollapsibleSection
              title="Last Deployment"
              defaultOpen={service.UpdateStatus?.State === "updating"}
            >
              <DeploymentChanges
                changes={changes}
                updateStatus={service.UpdateStatus}
              />
            </CollapsibleSection>
          )}
          {history.length > 0 && (
            <CollapsibleSection title="Recent Activity">
              <ActivityFeed
                entries={history}
                hideType
              />
            </CollapsibleSection>
          )}
        </div>
      )}

      {/* Container configuration */}
      <CollapsibleSection
        title="Container Configuration"
        defaultOpen={containerConfig != null}
      >
        {containerConfig ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <CommandEditor
              serviceId={id!}
              config={containerConfig}
              onSaved={setContainerConfig}
              canEdit={canPatch}
            />
            <RuntimeEditor
              serviceId={id!}
              config={containerConfig}
              onSaved={setContainerConfig}
              canEdit={canPatch}
            />
            <CapabilitiesEditor
              serviceId={id!}
              config={containerConfig}
              onSaved={setContainerConfig}
              canEdit={canPatch}
            />
            <ExtraHostsEditor
              serviceId={id!}
              config={containerConfig}
              onSaved={setContainerConfig}
              canEdit={canPatch}
            />
            <DnsEditor
              serviceId={id!}
              config={containerConfig}
              onSaved={setContainerConfig}
              canEdit={canPatch}
            />
          </div>
        ) : (
          <div className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">
            Loading…
          </div>
        )}
      </CollapsibleSection>

      {/* Environment variables */}
      {envVars !== null && (hasEnvContent || canPatch) && (
        <EnvEditor
          serviceId={id!}
          envVars={envVars}
          onSaved={setEnvVars}
          canEdit={canPatch}
        />
      )}

      {/* Integrations */}
      {integrations.map((integration) => {
        const rawLabels = rawLabelsForIntegration(serviceLabels, integration.name);

        const panelProps = {
          rawLabels,
          serviceId: id!,
          onSaved: setServiceLabels,
          editable: canPatch,
        };

        switch (integration.name) {
          case "traefik":
            return (
              <TraefikPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "shepherd":
            return (
              <ShepherdPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "swarm-cronjob":
            return (
              <CronjobPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          case "diun":
            return (
              <DiunPanel
                key={integration.name}
                integration={integration}
                {...panelProps}
              />
            );
          default:
            return null;
        }
      })}

      {/* Labels */}
      {serviceLabels !== null && (hasLabelsContent || canPatch) && (
        <KeyValueEditor
          title="Labels"
          entries={filteredLabels ?? {}}
          defaultOpen={Object.keys(filteredLabels ?? {}).length > 0}
          keyPlaceholder="com.example.my-label"
          valuePlaceholder="value"
          editDisabled={!canPatch}
          isKeyReadOnly={isReservedLabelKey}
          validateKey={validateLabelKey}
          onSave={async (ops) => {
            const updated = await api.patchServiceLabels(id!, ops);
            setServiceLabels(updated);
            return updated;
          }}
        />
      )}

      {/* Healthcheck */}
      {healthcheck !== undefined && (hasHealthcheckContent || canPatch) && (
        <HealthcheckEditor
          serviceId={id!}
          healthcheck={healthcheck}
          onSaved={setHealthcheck}
          canEdit={canPatch}
        />
      )}

      {/* Ports */}
      {specPorts !== null && (hasPortsContent || canPatch) && (
        <PortsEditor
          serviceId={id!}
          ports={specPorts}
          onSaved={setSpecPorts}
          canEdit={canPatch}
        />
      )}

      {serviceMounts !== null && (
        <MountsEditor
          serviceId={id!}
          mounts={serviceMounts}
          onSaved={setServiceMounts}
          canEdit={canPatch}
        />
      )}

      {/* Networks */}
      <NetworksEditor
        serviceId={id!}
        networks={(taskTemplate?.Networks ?? []).map(({ Target, Aliases }) => ({
          target: Target,
          aliases: Aliases ?? undefined,
        }))}
        networkNames={networkNames}
        onSaved={refetchService}
        canEdit={canPatch}
      />

      {/* Configs */}
      <ConfigsEditor
        serviceId={id!}
        configs={(containerSpec?.Configs ?? []).map((cfg) => ({
          configID: cfg.ConfigID,
          configName: cfg.ConfigName,
          fileName: cfg.File?.Name ?? "",
        }))}
        onSaved={refetchService}
        canEdit={canPatch}
      />

      {/* Secrets */}
      <SecretsEditor
        serviceId={id!}
        secrets={(containerSpec?.Secrets ?? []).map((sec) => ({
          secretID: sec.SecretID,
          secretName: sec.SecretName,
          fileName: sec.File?.Name ?? "",
        }))}
        onSaved={refetchService}
        canEdit={canPatch}
      />

      {/* Deploy: Resources, Placement, Restart, Update, Rollback */}
      <CollapsibleSection
        title="Deploy Configuration"
        defaultOpen={false}
      >
        <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2">
          <div className="flex flex-col gap-4">
            {service.Spec.EndpointSpec?.Mode && (
              <div className="flex flex-col gap-3 rounded-lg border p-3">
                <EndpointModeEditor
                  serviceId={id!}
                  currentMode={service.Spec.EndpointSpec.Mode as "vip" | "dnsrr"}
                  canEdit={canChangeEndpointMode}
                />
              </div>
            )}

            {serviceResources !== null && (hasResourcesContent || canPatch) && (
              <div
                id="resources-section"
                className="flex flex-col gap-3 rounded-lg border p-3"
              >
                <ResourcesEditor
                  serviceId={id!}
                  resources={serviceResources}
                  onSaved={setServiceResources}
                  canEdit={canPatch}
                  pids={taskTemplate.Resources?.Limits?.Pids}
                  allocation={{
                    cpuReserved,
                    cpuLimit,
                    cpuActual,
                    memReserved,
                    memLimit,
                    memActual,
                  }}
                />
              </div>
            )}

            {(hasPlacementContent || canPatch) && (
              <div className="flex flex-col gap-3 rounded-lg border p-3">
                <PlacementEditor
                  serviceId={id!}
                  placement={taskTemplate.Placement ?? null}
                  onSaved={refetchService}
                  canEdit={canPatch}
                />
              </div>
            )}

            {taskTemplate.RestartPolicy && (
              <div className="flex flex-col gap-3 rounded-lg border p-3">
                <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                  Restart Policy
                </h3>
                <KVTable
                  rows={[
                    taskTemplate.RestartPolicy.Condition && [
                      "Condition",
                      taskTemplate.RestartPolicy.Condition,
                    ],
                    taskTemplate.RestartPolicy.Delay != null && [
                      "Delay",
                      formatDuration(taskTemplate.RestartPolicy.Delay),
                    ],
                    taskTemplate.RestartPolicy.MaxAttempts != null && [
                      "Max Attempts",
                      String(taskTemplate.RestartPolicy.MaxAttempts),
                    ],
                    taskTemplate.RestartPolicy.Window != null && [
                      "Window",
                      formatDuration(taskTemplate.RestartPolicy.Window),
                    ],
                  ]}
                />
              </div>
            )}
          </div>

          <div className="flex flex-col gap-4">
            <LogDriverEditor
              serviceId={id!}
              logDriver={taskTemplate.LogDriver ?? null}
              onSaved={refetchService}
              canEdit={canPatch}
            />

            {(service.Spec.UpdateConfig || canPatch) && (
              <div className="flex flex-col gap-3 rounded-lg border p-3">
                <PolicyEditor
                  type="update"
                  serviceId={id!}
                  policy={service.Spec.UpdateConfig ?? null}
                  onSaved={refetchService}
                  canEdit={canPatch}
                />
              </div>
            )}

            {(service.Spec.RollbackConfig || canPatch) && (
              <div className="flex flex-col gap-3 rounded-lg border p-3">
                <PolicyEditor
                  type="rollback"
                  serviceId={id!}
                  policy={service.Spec.RollbackConfig ?? null}
                  onSaved={refetchService}
                  canEdit={canPatch}
                />
              </div>
            )}
          </div>
        </div>
      </CollapsibleSection>

      <ErrorBoundary inline>
        <LogViewer
          serviceId={id!}
          header="Logs"
        />
      </ErrorBoundary>
    </div>
  );
}

function ServiceStatusCard({ service }: { service: Service }) {
  const { label, state } = serviceUpdateStatus(service);
  const ts = service.UpdateStatus?.CompletedAt || service.UpdateStatus?.StartedAt;
  const msg = service.UpdateStatus?.Message;

  return (
    <InfoCard
      label="Status"
      value={
        <div className="flex flex-col">
          <span
            data-state={state}
            className="text-base font-medium text-green-600 data-[state=paused]:text-amber-600 data-[state=rollback_completed]:text-amber-600 data-[state=rollback_paused]:text-amber-600 data-[state=rollback_started]:text-amber-600 data-[state=updating]:text-blue-600 dark:text-green-400 dark:data-[state=paused]:text-amber-400 dark:data-[state=rollback_completed]:text-amber-400 dark:data-[state=rollback_paused]:text-amber-400 dark:data-[state=rollback_started]:text-amber-400 dark:data-[state=updating]:text-blue-400"
          >
            {label}
          </span>
          {ts && <span className="text-xs text-muted-foreground">{formatRelativeDate(ts)}</span>}
          {msg && label !== "Stable" && (
            <Tooltip>
              <TooltipTrigger
                render={<span className="truncate text-xs text-muted-foreground">{msg}</span>}
              />
              <TooltipContent>{msg}</TooltipContent>
            </Tooltip>
          )}
        </div>
      }
    />
  );
}
