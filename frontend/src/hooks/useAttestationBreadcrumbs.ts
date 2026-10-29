import { type AttestedKind, api } from "@/api/client";
import { stackNamespaceLabel } from "@/lib/parseStackLabels";
import { type Crumb, resourceBreadcrumbs } from "@/lib/resourceBreadcrumbs";
import { useQuery } from "@tanstack/react-query";

/**
 * The trail of a page about a service's or task's image, through the service
 * as its own detail page shows it. Until the service loads, it is named by id.
 */
export function useAttestationBreadcrumbs(kind: AttestedKind, id: string, page: string): Crumb[] {
  const { data } = useQuery({
    queryKey: ["attested-resource", kind, id],
    queryFn: async ({ signal }) => {
      if (kind === "service") {
        const { data: detail } = await api.service(id, signal);

        return { service: detail.service, task: undefined };
      }

      const { data: task } = await api.task(id, signal);
      const service = await api
        .service(task.ServiceID, signal)
        .then(({ data: detail }) => detail.service)
        .catch(() => undefined);

      return { service, task };
    },
  });

  const service = data?.service;
  const task = data?.task;
  const serviceId = task?.ServiceID ?? (kind === "service" ? id : undefined);
  const trail: Crumb[] = [{ label: page }];

  if (kind === "task") {
    trail.unshift({
      label: task?.Slot ? `Replica #${task.Slot}` : id.slice(0, 12),
      to: `/tasks/${id}`,
    });
  }

  return resourceBreadcrumbs({
    listLabel: "Services",
    listPath: "/services",
    name: service?.Spec.Name ?? task?.ServiceName ?? serviceId?.slice(0, 12) ?? "",
    stack: service?.Spec.Labels?.[stackNamespaceLabel],
    to: serviceId ? `/services/${serviceId}` : undefined,
    trail,
  });
}
