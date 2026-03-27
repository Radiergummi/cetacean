import type { TraefikIntegration, TraefikMiddleware, TraefikRouter, TraefikService } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import KeyValuePills from "@/components/data/KeyValuePills";
import { Lock } from "lucide-react";

interface TraefikPanelProps {
  integration: TraefikIntegration;
}

const badgeBase = "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium";
const badgeBlue = `${badgeBase} bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300`;
const badgePurple = `${badgeBase} bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300`;

function RouterCard({ router }: { router: TraefikRouter }) {
  return (
    <div className="flex flex-col gap-1.5 rounded-lg border px-3 py-2 text-sm">
      <span className="flex flex-wrap items-center gap-2">
        <span className="font-bold">{router.name}</span>

        {router.tls && (
          <span className="inline-flex items-center gap-1 text-xs text-green-700 dark:text-green-400">
            <Lock className="h-3 w-3" />
            {router.tls.certResolver && (
              <span>{router.tls.certResolver}</span>
            )}
          </span>
        )}
      </span>

      {router.rule && (
        <code className="text-xs font-mono text-muted-foreground break-all">{router.rule}</code>
      )}

      <span className="flex flex-wrap items-center gap-1.5">
        {router.entrypoints?.map((entrypoint) => (
          <span key={entrypoint} className={badgeBlue}>{entrypoint}</span>
        ))}

        {router.middlewares?.map((middleware) => (
          <span key={middleware} className={badgePurple}>{middleware}</span>
        ))}
      </span>

      {router.service && (
        <span className="text-xs text-muted-foreground">
          &rarr; {router.service}
        </span>
      )}
    </div>
  );
}

function ServiceRow({ service }: { service: TraefikService }) {
  return (
    <span className="inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm">
      <span className="font-bold">{service.name}</span>

      {service.port != null && (
        <span className="text-xs text-muted-foreground">:{service.port}</span>
      )}

      {service.scheme && (
        <span className={badgeBlue}>{service.scheme}</span>
      )}
    </span>
  );
}

function MiddlewareRow({ middleware }: { middleware: TraefikMiddleware }) {
  return (
    <div className="flex flex-col gap-1.5 rounded-lg border px-3 py-2 text-sm">
      <span className="flex items-center gap-2">
        <span className="font-bold">{middleware.name}</span>
        <span className={badgeBlue}>{middleware.type}</span>
      </span>

      {middleware.config && Object.keys(middleware.config).length > 0 && (
        <KeyValuePills entries={Object.entries(middleware.config)} />
      )}
    </div>
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
        <div className="flex flex-col gap-4">
          {hasRouters && (
            <div className="flex flex-col gap-2">
              <div className="text-xs font-medium tracking-wider text-muted-foreground uppercase">Routers</div>
              <div className="flex flex-wrap gap-2">
                {routers.map((router) => (
                  <RouterCard key={router.name} router={router} />
                ))}
              </div>
            </div>
          )}

          {hasServices && (
            <div className="flex flex-col gap-2">
              <div className="text-xs font-medium tracking-wider text-muted-foreground uppercase">Services</div>
              <div className="flex flex-wrap gap-2">
                {services.map((service) => (
                  <ServiceRow key={service.name} service={service} />
                ))}
              </div>
            </div>
          )}

          {hasMiddlewares && (
            <div className="flex flex-col gap-2">
              <div className="text-xs font-medium tracking-wider text-muted-foreground uppercase">Middlewares</div>
              <div className="flex flex-col gap-2">
                {middlewares.map((middleware) => (
                  <MiddlewareRow key={middleware.name} middleware={middleware} />
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </CollapsibleSection>
  );
}
