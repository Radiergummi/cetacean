import type { ShepherdIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { IntegrationSection } from "./IntegrationSection";

const docsUrl = "https://github.com/djmaze/shepherd#usage";

/**
 * Read-only panel displaying parsed Shepherd auto-update configuration.
 */
export function ShepherdPanel({
  integration,
  rawLabels,
}: {
  integration: ShepherdIntegration;
  rawLabels: [string, string][];
}) {
  const { enabled, authConfig } = integration;

  return (
    <IntegrationSection title="Shepherd" defaultOpen={enabled} rawLabels={rawLabels} docsUrl={docsUrl}>
      {!enabled && (
        <p className="text-sm text-muted-foreground">Disabled</p>
      )}

      {enabled && (
        <KVTable
          rows={[
            authConfig && ["Auth config", authConfig],
          ]}
        />
      )}
    </IntegrationSection>
  );
}
