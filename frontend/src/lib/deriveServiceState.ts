import type { ContainerConfig, Healthcheck, PortConfig, ServiceMount, Service } from "@/api/types";
import type { ServiceResourceShape } from "@/components/service-detail";

export interface DerivedServiceState {
  envVars: Record<string, string>;
  serviceResources: ServiceResourceShape | null;
  serviceLabels: Record<string, string>;
  healthcheck: Healthcheck | null;
  specPorts: PortConfig[];
  serviceMounts: ServiceMount[];
  containerConfig: ContainerConfig;
}

/**
 * Derives all sub-resource state from a Service object.
 * Replicates the server-side transformations that the sub-resource
 * GET endpoints perform (envSliceToMap, extractConfigRefs, etc.).
 */
export function deriveServiceSubResources(service: Service): DerivedServiceState {
  const spec = service.Spec;
  const taskTemplate = spec.TaskTemplate;
  const containerSpec = taskTemplate.ContainerSpec;

  return {
    envVars: envSliceToMap(containerSpec.Env),
    serviceResources: taskTemplate.Resources ?? null,
    serviceLabels: spec.Labels ?? {},
    healthcheck: containerSpec.Healthcheck ?? null,
    specPorts: spec.EndpointSpec?.Ports ?? [],
    serviceMounts: containerSpec.Mounts ?? [],
    containerConfig: containerConfigFromSpec(containerSpec),
  };
}

function envSliceToMap(env?: string[]): Record<string, string> {
  const result: Record<string, string> = {};

  if (!env) {
    return result;
  }

  for (const entry of env) {
    const index = entry.indexOf("=");

    if (index >= 0) {
      result[entry.substring(0, index)] = entry.substring(index + 1);
    } else {
      result[entry] = "";
    }
  }

  return result;
}

function containerConfigFromSpec(
  spec: Service["Spec"]["TaskTemplate"]["ContainerSpec"],
): ContainerConfig {
  return {
    command: spec.Command,
    args: spec.Args,
    dir: spec.Dir ?? "",
    user: spec.User ?? "",
    hostname: spec.Hostname ?? "",
    init: spec.Init,
    tty: spec.TTY ?? false,
    readOnly: spec.ReadOnly ?? false,
    stopSignal: spec.StopSignal ?? "",
    stopGracePeriod: spec.StopGracePeriod,
    capabilityAdd: spec.CapabilityAdd,
    capabilityDrop: spec.CapabilityDrop,
    groups: spec.Groups,
    hosts: spec.Hosts,
    dnsConfig: spec.DNSConfig
      ? {
          nameservers: spec.DNSConfig.Nameservers,
          search: spec.DNSConfig.Search,
          options: spec.DNSConfig.Options,
        }
      : undefined,
  };
}
