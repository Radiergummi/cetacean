import { IntegrationSection } from "./IntegrationSection";
import type { CaddyIntegration } from "@/api/types";
import { CopyButton } from "@/components/ui/copy-button";

const docsUrl = "https://github.com/lucaslorentz/caddy-docker-proxy#labels-to-caddyfile-conversion";

/**
 * Panel displaying the Caddyfile caddy-docker-proxy builds from a service's
 * labels. Template expressions are shown as written, not evaluated.
 */
export function CaddyPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: CaddyIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean | undefined;
}) {
  return (
    <IntegrationSection
      title="Caddy"
      defaultOpen
      enabled
      rawLabels={rawLabels}
      docsUrl={docsUrl}
      editable={editable}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      <div className="relative overflow-hidden rounded-lg border">
        <div className="flex items-center justify-between ps-3 pe-1.5 pt-1.5">
          <span className="text-[10px] font-medium tracking-wider text-muted-foreground uppercase">
            Caddyfile
          </span>
          <CopyButton text={integration.caddyfile} />
        </div>

        <pre className="overflow-x-auto p-4 font-mono text-sm whitespace-pre">
          <code>{integration.caddyfile}</code>
        </pre>
      </div>
    </IntegrationSection>
  );
}
