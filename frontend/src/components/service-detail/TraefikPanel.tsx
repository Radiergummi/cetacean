import { api } from "@/api/client";
import type {
  TraefikIntegration,
  TraefikMiddleware,
  TraefikRouter,
  TraefikService,
} from "@/api/types";
import KeyValuePills from "@/components/data/KeyValuePills";
import { Input } from "@/components/ui/input";
import { MultiCombobox } from "@/components/ui/multi-combobox";
import { Switch } from "@/components/ui/switch";
import { diffLabels } from "@/lib/integrationLabels";
import { ArrowRight, Lock } from "lucide-react";
import { useState } from "react";
import { IntegrationSection } from "./IntegrationSection";

const docsUrl = "https://doc.traefik.io/traefik/providers/swarm/#routing-configuration-with-labels";

const badgeBase =
  "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium";
const badgeBlue = `${badgeBase} bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300`;
const badgePurple = `${badgeBase} bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300`;
const badgeTeal = `${badgeBase} bg-teal-100 text-teal-800 dark:bg-teal-900/30 dark:text-teal-300`;

// ── Display components ────────────────────────────────────────────

function RouterCard({ router }: { router: TraefikRouter }) {
  return (
    <article className="flex flex-col gap-1.5 rounded-lg border px-3 py-2 text-sm">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <span className="font-medium">{router.name}</span>

        {router.tls && (
          <span className="inline-flex items-center gap-1 text-xs text-green-700 dark:text-green-400">
            <Lock className="h-3 w-3" />
            {router.tls.certResolver && <span>{router.tls.certResolver}</span>}
          </span>
        )}
      </header>

      {router.rule && (
        <code className="text-xs font-mono text-muted-foreground break-all bg-muted rounded-md p-2">
          {router.rule}
        </code>
      )}

      <span className="flex flex-wrap items-center gap-1.5">
        {router.middlewares?.map((middleware) => (
          <span key={middleware} className={badgePurple}>
            {middleware}
          </span>
        ))}
      </span>

      {(router.entrypoints?.length || router.service) && (
        <footer className="flex items-center gap-1 mt-1">
          <ArrowRight className="size-3" />
          {router.entrypoints?.map((entrypoint) => (
            <span key={entrypoint} className={badgeTeal}>
              {entrypoint}
            </span>
          ))}

          {router.service && (
            <>
              <span className="text-xs text-muted-foreground leading-none ms-auto">
                {router.service}
              </span>
              <ArrowRight className="size-3" />
            </>
          )}
        </footer>
      )}
    </article>
  );
}

function ServiceRow({ service }: { service: TraefikService }) {
  return (
    <article className="flex justify-between items-center gap-2 rounded-lg border px-3 py-2 text-sm">
      <header>
        <span className="font-medium">{service.name}</span>
      </header>

      {service.port != null && (
        <span className="text-xs text-muted-foreground me-auto bg-muted py-0.5 px-1.5 rounded-sm">
          :{service.port}
        </span>
      )}

      {service.scheme && <span className={badgeBlue}>{service.scheme}</span>}
    </article>
  );
}

function MiddlewareRow({ middleware }: { middleware: TraefikMiddleware }) {
  return (
    <article className="flex flex-col gap-1.5 rounded-lg border px-3 py-2 text-sm">
      <header className="flex items-center justify-between gap-2">
        <span className="font-medium">{middleware.name}</span>
        <span className={badgeBlue}>{middleware.type}</span>
      </header>

      {middleware.config && Object.keys(middleware.config).length > 0 && (
        <KeyValuePills entries={Object.entries(middleware.config)} />
      )}
    </article>
  );
}

// ── Edit components ───────────────────────────────────────────────

interface RouterFormState {
  name: string;
  rule: string;
  entrypoints: string[];
  middlewares: string[];
  service: string;
  priority: string;
  certResolver: string;
}

interface ServiceFormState {
  name: string;
  port: string;
  scheme: string;
}

interface MiddlewareFormState {
  name: string;
  type: string;
  config: [string, string][];
}

function RouterEditCard({
  state,
  onChange,
}: {
  state: RouterFormState;
  onChange: (updated: RouterFormState) => void;
}) {
  return (
    <article className="space-y-3 rounded-lg border p-3">
      <header className="font-medium text-muted-foreground">{state.name}</header>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Rule</label>
        <Input
          className="font-mono"
          value={state.rule}
          onChange={(event) => onChange({ ...state, rule: event.target.value })}
        />
        <p className="text-xs text-muted-foreground">Routing rule expression, e.g. Host(`example.com`)</p>
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Entrypoints</label>
        <MultiCombobox
          values={state.entrypoints}
          onChange={(entrypoints) => onChange({ ...state, entrypoints })}
          options={[]}
          placeholder="Add entrypoint..."
        />
        <p className="text-xs text-muted-foreground">Entrypoints this router listens on</p>
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Middlewares</label>
        <MultiCombobox
          values={state.middlewares}
          onChange={(middlewares) => onChange({ ...state, middlewares })}
          options={[]}
          placeholder="Add middleware..."
        />
        <p className="text-xs text-muted-foreground">Middleware names to apply to this router</p>
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Service</label>
        <Input
          value={state.service}
          onChange={(event) => onChange({ ...state, service: event.target.value })}
        />
        <p className="text-xs text-muted-foreground">Backend Traefik service to forward requests to</p>
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Priority</label>
        <Input
          type="number"
          className="w-24"
          value={state.priority}
          onChange={(event) => onChange({ ...state, priority: event.target.value })}
        />
        <p className="text-xs text-muted-foreground">Higher values win on rule conflicts</p>
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">TLS cert resolver</label>
        <Input
          value={state.certResolver}
          onChange={(event) => onChange({ ...state, certResolver: event.target.value })}
          placeholder="letsencrypt"
        />
        <p className="text-xs text-muted-foreground">Certificate resolver for automatic TLS</p>
      </div>
    </article>
  );
}

function ServiceEditCard({
  state,
  onChange,
}: {
  state: ServiceFormState;
  onChange: (updated: ServiceFormState) => void;
}) {
  return (
    <article className="space-y-3 rounded-lg border p-3">
      <header className="font-medium text-muted-foreground">{state.name}</header>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Port</label>
        <Input
          type="number"
          className="w-24"
          value={state.port}
          onChange={(event) => onChange({ ...state, port: event.target.value })}
        />
        <p className="text-xs text-muted-foreground">Backend server port for load balancing</p>
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Scheme</label>
        <Input
          className="w-32"
          value={state.scheme}
          onChange={(event) => onChange({ ...state, scheme: event.target.value })}
          placeholder="http"
        />
        <p className="text-xs text-muted-foreground">Backend protocol (http, https, or h2c)</p>
      </div>
    </article>
  );
}

function MiddlewareEditCard({
  state,
  onChange,
}: {
  state: MiddlewareFormState;
  onChange: (updated: MiddlewareFormState) => void;
}) {
  return (
    <article className="space-y-3 rounded-lg border p-3">
      <header className="flex items-center justify-between gap-2">
        <span className="font-medium text-muted-foreground">{state.name}</span>
        <span className={badgeBlue}>{state.type}</span>
      </header>

      {state.config.map(([key, value], index) => (
        <div key={key} className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-foreground font-mono">{key}</label>
          <Input
            value={value}
            onChange={(event) => {
              const updated = [...state.config] as [string, string][];
              updated[index] = [key, event.target.value];
              onChange({ ...state, config: updated });
            }}
          />
        </div>
      ))}
    </article>
  );
}

// ── Serialization ─────────────────────────────────────────────────

function serializeTraefikLabels(
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

    if (router.priority.trim() && router.priority !== "0") {
      labels[`${prefix}.priority`] = router.priority;
    }

    if (router.certResolver.trim()) {
      labels[`${prefix}.tls`] = "true";
      labels[`${prefix}.tls.certresolver`] = router.certResolver;
    }
  }

  for (const service of serviceForms) {
    const prefix = `traefik.http.services.${service.name}.loadbalancer.server`;

    if (service.port.trim()) {
      labels[`${prefix}.port`] = service.port;
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

// ── Main component ────────────────────────────────────────────────

/**
 * Panel displaying parsed Traefik integration data for a service,
 * with optional inline editing support.
 */
export function TraefikPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: TraefikIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean;
}) {
  const { enabled, routers, services, middlewares } = integration;

  const hasRouters = routers && routers.length > 0;
  const hasServices = services && services.length > 0;
  const hasMiddlewares = middlewares && middlewares.length > 0;

  const [formEnabled, setFormEnabled] = useState(true);
  const [routerForms, setRouterForms] = useState<RouterFormState[]>([]);
  const [serviceForms, setServiceForms] = useState<ServiceFormState[]>([]);
  const [middlewareForms, setMiddlewareForms] = useState<MiddlewareFormState[]>([]);

  function resetForm() {
    setFormEnabled(integration.enabled);

    setRouterForms(
      (integration.routers ?? []).map((router) => ({
        name: router.name,
        rule: router.rule ?? "",
        entrypoints: router.entrypoints ?? [],
        middlewares: router.middlewares ?? [],
        service: router.service ?? "",
        priority: router.priority ? String(router.priority) : "",
        certResolver: router.tls?.certResolver ?? "",
      })),
    );

    setServiceForms(
      (integration.services ?? []).map((service) => ({
        name: service.name,
        port: service.port ? String(service.port) : "",
        scheme: service.scheme ?? "",
      })),
    );

    setMiddlewareForms(
      (integration.middlewares ?? []).map((middleware) => ({
        name: middleware.name,
        type: middleware.type,
        config: Object.entries(middleware.config ?? {}),
      })),
    );
  }

  async function handleSave() {
    const newLabels = serializeTraefikLabels(formEnabled, routerForms, serviceForms, middlewareForms);
    const ops = diffLabels(rawLabels, newLabels);
    const updated = await api.patchServiceLabels(serviceId, ops);
    onSaved(updated);
  }

  function updateRouter(index: number, updated: RouterFormState) {
    setRouterForms((previous) =>
      previous.map((form, formIndex) => (formIndex === index ? updated : form)),
    );
  }

  function updateService(index: number, updated: ServiceFormState) {
    setServiceForms((previous) =>
      previous.map((form, formIndex) => (formIndex === index ? updated : form)),
    );
  }

  function updateMiddleware(index: number, updated: MiddlewareFormState) {
    setMiddlewareForms((previous) =>
      previous.map((form, formIndex) => (formIndex === index ? updated : form)),
    );
  }

  const editForm = (
    <div className="grid gap-4 lg:grid-cols-3">
      <div className="flex flex-col gap-1.5 lg:col-span-3">
        <label className="flex items-center gap-2">
          <Switch checked={formEnabled} onCheckedChange={setFormEnabled} />
          <span className="text-xs font-medium text-foreground">Enabled</span>
        </label>
        <p className="text-xs text-muted-foreground">Enable Traefik routing for this service</p>
      </div>

      {routerForms.length > 0 && (
        <section className="flex flex-col gap-2">
          <header className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Routers
          </header>
          {routerForms.map((form, index) => (
            <RouterEditCard
              key={form.name}
              state={form}
              onChange={(updated) => updateRouter(index, updated)}
            />
          ))}
        </section>
      )}

      {serviceForms.length > 0 && (
        <section className="flex flex-col gap-2">
          <header className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Services
          </header>
          {serviceForms.map((form, index) => (
            <ServiceEditCard
              key={form.name}
              state={form}
              onChange={(updated) => updateService(index, updated)}
            />
          ))}
        </section>
      )}

      {middlewareForms.length > 0 && (
        <section className="flex flex-col gap-2">
          <header className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Middlewares
          </header>
          {middlewareForms.map((form, index) => (
            <MiddlewareEditCard
              key={form.name}
              state={form}
              onChange={(updated) => updateMiddleware(index, updated)}
            />
          ))}
        </section>
      )}
    </div>
  );

  return (
    <IntegrationSection
      title="Traefik"
      defaultOpen={enabled}
      rawLabels={rawLabels}
      docsUrl={docsUrl}
      editable={editable}
      editContent={editForm}
      onEditStart={resetForm}
      onSave={handleSave}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      {!enabled && (
        <p className="text-sm text-muted-foreground">Disabled</p>
      )}

      {enabled && (
        <div className="grid gap-4 lg:grid-cols-3">
          {hasRouters && (
            <section className="flex flex-col gap-2">
              <header className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Routers
              </header>
              <ul className="flex flex-col gap-2">
                {routers.map((router) => (
                  <li key={router.name} className="contents">
                    <RouterCard router={router} />
                  </li>
                ))}
              </ul>
            </section>
          )}

          {hasServices && (
            <section className="flex flex-col gap-2">
              <header className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Services
              </header>
              <ul className="flex flex-col gap-2">
                {services.map((service) => (
                  <li key={service.name} className="contents">
                    <ServiceRow service={service} />
                  </li>
                ))}
              </ul>
            </section>
          )}

          {hasMiddlewares && (
            <section className="flex flex-col gap-2">
              <header className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Middlewares
              </header>
              <ul className="flex flex-col gap-2">
                {middlewares.map((middleware) => (
                  <li key={middleware.name} className="contents">
                    <MiddlewareRow middleware={middleware} />
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      )}
    </IntegrationSection>
  );
}
