import { api } from "../api/client";
import ActivityFeed from "../components/ActivityFeed";
import CollapsibleSection from "../components/CollapsibleSection";
import ComposeSection from "../components/ComposeSection";
import { ContainerImage, MetadataGrid, ResourceLink, Timestamp } from "../components/data";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
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
  EnvEditor,
  ExtraHostsEditor,
  HealthcheckEditor,
  MountsEditor,
  NetworksEditor,
  PortsEditor,
  ReplicaCard,
  RuntimeEditor,
  SecretsEditor,
  ServiceActions,
} from "../components/service-detail";
import { DeployConfigSection } from "../components/service-detail/DeployConfigSection";
import { IntegrationPanels } from "../components/service-detail/IntegrationPanels";
import { ServiceStatusCard } from "../components/service-detail/ServiceStatusCard";
import ServiceLinks from "../components/ServiceLinks";
import { SizingBanner } from "../components/SizingBanner";
import TasksTable from "../components/TasksTable";
import { useEditVersion } from "../hooks/useEditVersion";
import { useServiceDetail } from "../hooks/useServiceDetail";
import { isReservedLabelKey, validateLabelKey } from "../lib/labelValidation";
import { stackNamespaceLabel } from "../lib/parseStackLabels";
import { resourceBreadcrumbs } from "../lib/resourceBreadcrumbs";
import { useParams } from "react-router-dom";

export default function ServiceDetail() {
  const { id } = useParams<{ id: string }>();
  const detail = useServiceDetail(id);
  const labelsVersion = useEditVersion(`/services/${id}/labels`);

  if (detail.error) {
    return <FetchError message="Failed to load service" />;
  }

  if (!detail.service) {
    return <LoadingDetail />;
  }

  const service = detail.service;
  const taskTemplate = service.Spec.TaskTemplate;
  const containerSpec = taskTemplate?.ContainerSpec;
  const labels = service.Spec.Labels;

  const hasHealthcheckContent =
    detail.healthcheck != null &&
    !(detail.healthcheck.Test?.length === 1 && detail.healthcheck.Test[0] === "NONE");
  const hasPortsContent = detail.specPorts != null && detail.specPorts.length > 0;
  const hasEnvContent = detail.envVars != null && Object.keys(detail.envVars).length > 0;
  const hasLabelsContent =
    detail.filteredLabels != null && Object.keys(detail.filteredLabels).length > 0;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          <ResourceName
            name={detail.name}
            stack={labels?.[stackNamespaceLabel] ?? null}
            direction="responsive"
          />
        }
        breadcrumbs={resourceBreadcrumbs({
          listLabel: "Services",
          listPath: "/services",
          name: detail.name,
          stack: labels?.[stackNamespaceLabel],
        })}
        actions={
          <>
            <ServiceLinks links={detail.links} />
            <ServiceActions
              service={service}
              serviceId={id!}
              allowedMethods={detail.allowedMethods}
              editable={detail.editable}
            />
          </>
        }
      />

      <SizingBanner
        hints={detail.serviceRecommendations}
        editable={detail.editable}
        onFixed={detail.refetchService}
      />

      {/* Overview cards */}
      <MetadataGrid>
        <ContainerImage
          image={containerSpec?.Image ?? ""}
          serviceId={id}
          canEdit={detail.editable.has("image")}
          imageUpdate={detail.imageUpdate}
        />
        <ReplicaCard
          service={service}
          tasks={detail.tasks}
          canScale={detail.editable.has("scale")}
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
        tasks={detail.tasks}
        variant="service"
        metrics={detail.hasCadvisor ? detail.taskMetrics : undefined}
      />

      {detail.hasPrometheus && (
        <ErrorBoundary inline>
          <MetricsPanel
            header="Metrics"
            charts={detail.metricsCharts}
          />
        </ErrorBoundary>
      )}

      {(detail.changes.length > 0 || detail.history.length > 0) && (
        <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
          {detail.changes.length > 0 && (
            <CollapsibleSection
              title="Last Deployment"
              defaultOpen={service.UpdateStatus?.State === "updating"}
            >
              <DeploymentChanges
                changes={detail.changes}
                updateStatus={service.UpdateStatus}
              />
            </CollapsibleSection>
          )}
          {detail.history.length > 0 && (
            <CollapsibleSection title="Recent Activity">
              <ActivityFeed entries={detail.history} />
            </CollapsibleSection>
          )}
        </div>
      )}

      {/* Container configuration */}
      <CollapsibleSection
        title="Container Configuration"
        defaultOpen={detail.containerConfig != null}
      >
        {detail.containerConfig ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <CommandEditor
              serviceId={id!}
              config={detail.containerConfig}
              onSaved={detail.onContainerConfigSaved}
              canEdit={detail.editable.has("container-config")}
            />
            <RuntimeEditor
              serviceId={id!}
              config={detail.containerConfig}
              onSaved={detail.onContainerConfigSaved}
              canEdit={detail.editable.has("container-config")}
            />
            <CapabilitiesEditor
              serviceId={id!}
              config={detail.containerConfig}
              onSaved={detail.onContainerConfigSaved}
              canEdit={detail.editable.has("container-config")}
            />
            <ExtraHostsEditor
              serviceId={id!}
              config={detail.containerConfig}
              onSaved={detail.onContainerConfigSaved}
              canEdit={detail.editable.has("container-config")}
            />
            <DnsEditor
              serviceId={id!}
              config={detail.containerConfig}
              onSaved={detail.onContainerConfigSaved}
              canEdit={detail.editable.has("container-config")}
            />
          </div>
        ) : (
          <div className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">
            Loading…
          </div>
        )}
      </CollapsibleSection>

      {/* Environment variables */}
      {detail.envVars !== null && (hasEnvContent || detail.editable.has("env")) && (
        <EnvEditor
          serviceId={id!}
          envVars={detail.envVars}
          onSaved={detail.onEnvSaved}
          canEdit={detail.editable.has("env")}
        />
      )}

      {/* Integrations */}
      <IntegrationPanels
        integrations={detail.integrations}
        serviceLabels={detail.serviceLabels}
        serviceId={id!}
        onSaved={detail.onLabelsSaved}
        editable={detail.editable.has("labels")}
      />

      {/* Labels */}
      {detail.serviceLabels !== null && (hasLabelsContent || detail.editable.has("labels")) && (
        <KeyValueEditor
          title="Labels"
          entries={detail.filteredLabels ?? {}}
          defaultOpen={Object.keys(detail.filteredLabels ?? {}).length > 0}
          keyPlaceholder="com.example.my-label"
          valuePlaceholder="value"
          editDisabled={!detail.editable.has("labels")}
          isKeyReadOnly={isReservedLabelKey}
          validateKey={validateLabelKey}
          onEditStart={labelsVersion.capture}
          onSave={async (ops) => {
            const updated = await api.patchServiceLabels(id!, ops, await labelsVersion.ifMatch());
            detail.onLabelsSaved(updated);
            return updated;
          }}
        />
      )}

      {/* Healthcheck */}
      {detail.healthcheck !== undefined &&
        (hasHealthcheckContent || detail.editable.has("healthcheck")) && (
          <HealthcheckEditor
            serviceId={id!}
            healthcheck={detail.healthcheck}
            onSaved={detail.onHealthcheckSaved}
            canEdit={detail.editable.has("healthcheck")}
          />
        )}

      {/* Ports */}
      {detail.specPorts !== null && (hasPortsContent || detail.editable.has("ports")) && (
        <PortsEditor
          serviceId={id!}
          ports={detail.specPorts}
          onSaved={detail.onPortsSaved}
          canEdit={detail.editable.has("ports")}
        />
      )}

      {detail.serviceMounts !== null && (
        <MountsEditor
          serviceId={id!}
          mounts={detail.serviceMounts}
          onSaved={detail.onMountsSaved}
          canEdit={detail.editable.has("mounts")}
        />
      )}

      {/* Networks */}
      <NetworksEditor
        serviceId={id!}
        networks={(taskTemplate?.Networks ?? []).flatMap(({ Target, Aliases }) =>
          Target ? [{ target: Target, aliases: Aliases ?? undefined }] : [],
        )}
        networkNames={detail.networkNames}
        onSaved={detail.refetchService}
        canEdit={detail.editable.has("networks")}
      />

      {/* Configs */}
      <ConfigsEditor
        serviceId={id!}
        configs={(containerSpec?.Configs ?? []).map((cfg) => ({
          configID: cfg.ConfigID,
          configName: cfg.ConfigName,
          fileName: cfg.File?.Name ?? "",
        }))}
        onSaved={detail.refetchService}
        canEdit={detail.editable.has("configs")}
      />

      {/* Secrets */}
      <SecretsEditor
        serviceId={id!}
        secrets={(containerSpec?.Secrets ?? []).map((sec) => ({
          secretID: sec.SecretID,
          secretName: sec.SecretName,
          fileName: sec.File?.Name ?? "",
        }))}
        onSaved={detail.refetchService}
        canEdit={detail.editable.has("secrets")}
      />

      {/* Deploy: Resources, Placement, Restart, Update, Rollback */}
      <DeployConfigSection
        service={service}
        serviceId={id!}
        tasks={detail.tasks}
        editable={detail.editable}
        serviceResources={detail.serviceResources}
        onResourcesSaved={detail.onResourcesSaved}
        onRefetch={detail.refetchService}
        cpuActual={detail.cpuActual}
        memActual={detail.memActual}
      />

      <ComposeSection
        name={service.Spec.Name}
        queryKey={`service:${id!}`}
        fetcher={(signal) => api.serviceCompose(id!, signal)}
      />

      <ErrorBoundary inline>
        <LogViewer
          serviceId={id!}
          header="Logs"
        />
      </ErrorBoundary>
    </div>
  );
}
