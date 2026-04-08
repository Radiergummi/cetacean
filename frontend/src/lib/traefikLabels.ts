export interface RouterFormState {
  name: string;
  rule: string;
  entrypoints: string[];
  middlewares: string[];
  service: string;
  priority: number | undefined;
  certResolver: string;
}

export interface ServiceFormState {
  name: string;
  port: number | undefined;
  scheme: string;
}

export interface MiddlewareFormState {
  name: string;
  type: string;
  config: [string, string][];
}

/**
 * Serialize Traefik form state into Docker service labels.
 */
export function serializeTraefikLabels(
  formEnabled: boolean,
  routerForms: RouterFormState[],
  serviceForms: ServiceFormState[],
  middlewareForms: MiddlewareFormState[],
): Record<string, string> {
  const labels: Record<string, string> = {
    "traefik.enable": String(formEnabled),
  };

  for (const router of routerForms) {
    const prefix = `traefik.http.routers.${router.name}`;

    if (router.rule.trim()) {
      labels[`${prefix}.rule`] = router.rule;
    }

    if (router.entrypoints.length > 0) {
      labels[`${prefix}.entrypoints`] = router.entrypoints.join(",");
    }

    if (router.middlewares.length > 0) {
      labels[`${prefix}.middlewares`] = router.middlewares.join(",");
    }

    if (router.service.trim()) {
      labels[`${prefix}.service`] = router.service;
    }

    if (router.priority != null && router.priority > 0) {
      labels[`${prefix}.priority`] = String(router.priority);
    }

    if (router.certResolver.trim()) {
      labels[`${prefix}.tls`] = "true";
      labels[`${prefix}.tls.certresolver`] = router.certResolver;
    }
  }

  for (const service of serviceForms) {
    const prefix = `traefik.http.services.${service.name}.loadbalancer.server`;

    if (service.port != null) {
      labels[`${prefix}.port`] = String(service.port);
    }

    if (service.scheme.trim()) {
      labels[`${prefix}.scheme`] = service.scheme;
    }
  }

  for (const middleware of middlewareForms) {
    for (const [key, value] of middleware.config) {
      if (value.trim()) {
        labels[`traefik.http.middlewares.${middleware.name}.${middleware.type}.${key}`] = value;
      }
    }
  }

  return labels;
}
