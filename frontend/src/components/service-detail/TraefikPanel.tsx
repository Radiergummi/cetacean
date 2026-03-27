import type {
  TraefikIntegration,
  TraefikMiddleware,
  TraefikRouter,
  TraefikService,
} from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import KeyValuePills from "@/components/data/KeyValuePills";
import { ArrowRight, Lock } from "lucide-react";

interface TraefikPanelProps {
  integration: TraefikIntegration;
}

const badgeBase =
  "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium";
const badgeBlue = `${badgeBase} bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300`;
const badgePurple = `${badgeBase} bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300`;
const badgeTeal = `${badgeBase} bg-teal-100 text-teal-800 dark:bg-teal-900/30 dark:text-teal-300`;

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

/**
 * Read-only panel displaying parsed Traefik integration data for a service.
 */
export function TraefikPanel({ integration }: TraefikPanelProps) {
  const { enabled, routers, services, middlewares } = integration;

  const hasRouters = routers && routers.length > 0;
  const hasServices = services && services.length > 0;
  const hasMiddlewares = middlewares && middlewares.length > 0;

  return (
    <CollapsibleSection title="Traefik" defaultOpen={enabled}>
      {!enabled && (
        <span className="text-sm text-muted-foreground">Disabled</span>
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
    </CollapsibleSection>
  );
}
