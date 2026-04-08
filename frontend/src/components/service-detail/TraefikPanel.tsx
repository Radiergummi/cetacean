import { IntegrationSection } from "./IntegrationSection";
import type {
  TraefikIntegration,
  TraefikMiddleware,
  TraefikRouter,
  TraefikService,
} from "@/api/types";
import KeyValuePills from "@/components/data/KeyValuePills";
import { Input } from "@/components/ui/input";
import { MultiCombobox } from "@/components/ui/multi-combobox";
import { NumberField } from "@/components/ui/number-field";
import { Switch } from "@/components/ui/switch";
import { badgeBlue, badgePurple, badgeTeal, saveIntegrationLabels } from "@/lib/integrationLabels";
import {
  serializeTraefikLabels,
  type RouterFormState,
  type ServiceFormState,
  type MiddlewareFormState,
} from "@/lib/traefikLabels";
import { ArrowRight, Lock } from "lucide-react";
import { useState } from "react";

const docsUrl = "https://doc.traefik.io/traefik/providers/swarm/#routing-configuration-with-labels";

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
        <code className="rounded-md bg-muted p-2 font-mono text-xs break-all text-muted-foreground">
          {router.rule}
        </code>
      )}

      <span className="flex flex-wrap items-center gap-1.5">
        {router.middlewares?.map((middleware) => (
          <span
            key={middleware}
            className={badgePurple}
          >
            {middleware}
          </span>
        ))}
      </span>

      {(router.entrypoints?.length || router.service) && (
        <footer className="mt-1 flex items-center gap-1">
          <ArrowRight className="size-3" />
          {router.entrypoints?.map((entrypoint) => (
            <span
              key={entrypoint}
              className={badgeTeal}
            >
              {entrypoint}
            </span>
          ))}

          {router.service && (
            <>
              <span className="ms-auto text-xs leading-none text-muted-foreground">
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
    <article className="flex items-center justify-between gap-2 rounded-lg border px-3 py-2 text-sm">
      <header>
        <span className="font-medium">{service.name}</span>
      </header>

      {service.port != null && (
        <span className="me-auto rounded-sm bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
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

function initRouterForms(integration: TraefikIntegration): RouterFormState[] {
  return (integration.routers ?? []).map((router) => ({
    name: router.name,
    rule: router.rule ?? "",
    entrypoints: router.entrypoints ?? [],
    middlewares: router.middlewares ?? [],
    service: router.service ?? "",
    priority: router.priority || undefined,
    certResolver: router.tls?.certResolver ?? "",
  }));
}

function initServiceForms(integration: TraefikIntegration): ServiceFormState[] {
  return (integration.services ?? []).map((service) => ({
    name: service.name,
    port: service.port || undefined,
    scheme: service.scheme ?? "",
  }));
}

function initMiddlewareForms(integration: TraefikIntegration): MiddlewareFormState[] {
  return (integration.middlewares ?? []).map((middleware) => ({
    name: middleware.name,
    type: middleware.type,
    config: Object.entries(middleware.config ?? {}),
  }));
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
        <textarea
          className="min-h-16 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          value={state.rule}
          onChange={(event) => onChange({ ...state, rule: event.target.value })}
        />
        <p className="text-xs text-muted-foreground">
          Routing rule expression, e.g. Host(`example.com`)
        </p>
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
        <p className="text-xs text-muted-foreground">
          Backend Traefik service to forward requests to
        </p>
      </div>

      <NumberField
        label="Priority"
        value={state.priority}
        onChange={(priority) => onChange({ ...state, priority })}
        min={0}
      />
      <p className="text-xs text-muted-foreground">Higher values win on rule conflicts</p>

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

      <NumberField
        label="Port"
        value={state.port}
        onChange={(port) => onChange({ ...state, port })}
        min={1}
        format={{ useGrouping: false }}
      />
      <p className="text-xs text-muted-foreground">Backend server port for load balancing</p>

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
        <div
          key={key}
          className="flex flex-col gap-1.5"
        >
          <label className="font-mono text-xs font-medium text-foreground">{key}</label>
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

  const [formEnabled, setFormEnabled] = useState(integration.enabled);
  const [routerForms, setRouterForms] = useState<RouterFormState[]>(() =>
    initRouterForms(integration),
  );
  const [serviceForms, setServiceForms] = useState<ServiceFormState[]>(() =>
    initServiceForms(integration),
  );
  const [middlewareForms, setMiddlewareForms] = useState<MiddlewareFormState[]>(() =>
    initMiddlewareForms(integration),
  );

  function resetForm() {
    setFormEnabled(integration.enabled);
    setRouterForms(initRouterForms(integration));
    setServiceForms(initServiceForms(integration));
    setMiddlewareForms(initMiddlewareForms(integration));
  }

  async function handleSave() {
    const newLabels = serializeTraefikLabels(
      formEnabled,
      routerForms,
      serviceForms,
      middlewareForms,
    );
    await saveIntegrationLabels(rawLabels, newLabels, serviceId, onSaved);
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
          <Switch
            checked={formEnabled}
            onCheckedChange={setFormEnabled}
          />
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
      enabled={enabled}
      rawLabels={rawLabels}
      docsUrl={docsUrl}
      editable={editable}
      editContent={editForm}
      onEditStart={resetForm}
      onSave={handleSave}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      <div className="grid gap-4 lg:grid-cols-3">
        {hasRouters && (
          <section className="flex flex-col gap-2">
            <header className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
              Routers
            </header>
            <ul className="flex flex-col gap-2">
              {routers.map((router) => (
                <li
                  key={router.name}
                  className="contents"
                >
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
                <li
                  key={service.name}
                  className="contents"
                >
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
                <li
                  key={middleware.name}
                  className="contents"
                >
                  <MiddlewareRow middleware={middleware} />
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>
    </IntegrationSection>
  );
}
