import type { Service, Task } from "../../api/types";
import { formatDuration } from "../../lib/format";
import CollapsibleSection from "../CollapsibleSection";
import { KVTable } from "../data";
import { EndpointModeEditor } from "./EndpointModeEditor";
import { LogDriverEditor } from "./LogDriverEditor";
import { PlacementEditor } from "./PlacementEditor";
import { PolicyEditor } from "./PolicyEditor";
import { ResourcesEditor } from "./ResourcesEditor";
import type { ServiceResourceShape } from "./ResourcesEditor";
import { useMemo } from "react";

interface DeployConfigSectionProps {
  service: Service;
  serviceId: string;
  tasks: Task[];
  canPatch: boolean;
  canChangeEndpointMode: boolean;
  serviceResources: ServiceResourceShape | null;
  onResourcesSaved: (resources: ServiceResourceShape) => void;
  onRefetch: () => void;
  cpuActual: number | undefined;
  memActual: number | undefined;
}

export function DeployConfigSection({
  service,
  serviceId,
  tasks,
  canPatch,
  canChangeEndpointMode,
  serviceResources,
  onResourcesSaved,
  onRefetch,
  cpuActual,
  memActual,
}: DeployConfigSectionProps) {
  const taskTemplate = service.Spec.TaskTemplate;
  const placement = taskTemplate?.Placement;

  const hasPlacementContent =
    (placement?.Constraints && placement.Constraints.length > 0) ||
    (placement?.Preferences && placement.Preferences.length > 0) ||
    (placement?.MaxReplicas != null && placement.MaxReplicas > 0);

  const hasResourcesContent =
    serviceResources != null &&
    (serviceResources.Limits?.NanoCPUs != null ||
      serviceResources.Limits?.MemoryBytes != null ||
      serviceResources.Reservations?.NanoCPUs != null ||
      serviceResources.Reservations?.MemoryBytes != null ||
      taskTemplate?.Resources?.Limits?.Pids != null);

  const allocation = useMemo(() => {
    const runningTasks = tasks.filter(({ Status }) => Status?.State === "running").length;
    const resources = service.Spec.TaskTemplate?.Resources;

    return {
      cpuReserved: resources?.Reservations?.NanoCPUs
        ? (resources.Reservations.NanoCPUs / 1e9) * 100 * runningTasks
        : undefined,
      cpuLimit: resources?.Limits?.NanoCPUs
        ? (resources.Limits.NanoCPUs / 1e9) * 100 * runningTasks
        : undefined,
      cpuActual,
      memReserved: resources?.Reservations?.MemoryBytes
        ? resources.Reservations.MemoryBytes * runningTasks
        : undefined,
      memLimit: resources?.Limits?.MemoryBytes
        ? resources.Limits.MemoryBytes * runningTasks
        : undefined,
      memActual,
    };
  }, [tasks, service, cpuActual, memActual]);

  return (
    <CollapsibleSection
      title="Deploy Configuration"
      defaultOpen={false}
    >
      <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2">
        <div className="flex flex-col gap-4">
          {service.Spec.EndpointSpec?.Mode && (
            <div className="flex flex-col gap-3 rounded-lg border p-3">
              <EndpointModeEditor
                serviceId={serviceId}
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
                serviceId={serviceId}
                resources={serviceResources}
                onSaved={onResourcesSaved}
                canEdit={canPatch}
                pids={taskTemplate.Resources?.Limits?.Pids}
                allocation={allocation}
              />
            </div>
          )}

          {(hasPlacementContent || canPatch) && (
            <div className="flex flex-col gap-3 rounded-lg border p-3">
              <PlacementEditor
                serviceId={serviceId}
                placement={taskTemplate.Placement ?? null}
                onSaved={onRefetch}
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
            serviceId={serviceId}
            logDriver={taskTemplate.LogDriver ?? null}
            onSaved={onRefetch}
            canEdit={canPatch}
          />

          {(service.Spec.UpdateConfig || canPatch) && (
            <div className="flex flex-col gap-3 rounded-lg border p-3">
              <PolicyEditor
                type="update"
                serviceId={serviceId}
                policy={service.Spec.UpdateConfig ?? null}
                onSaved={onRefetch}
                canEdit={canPatch}
              />
            </div>
          )}

          {(service.Spec.RollbackConfig || canPatch) && (
            <div className="flex flex-col gap-3 rounded-lg border p-3">
              <PolicyEditor
                type="rollback"
                serviceId={serviceId}
                policy={service.Spec.RollbackConfig ?? null}
                onSaved={onRefetch}
                canEdit={canPatch}
              />
            </div>
          )}
        </div>
      </div>
    </CollapsibleSection>
  );
}
