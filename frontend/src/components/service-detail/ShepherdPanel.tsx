import type { ShepherdIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { IntegrationSection } from "./IntegrationSection";

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
  const { enabled, schedule, imageFilter, latest, updateOpts } = integration;

  return (
    <IntegrationSection title="Shepherd" defaultOpen={enabled} rawLabels={rawLabels}>
      {!enabled && (
        <p className="text-sm text-muted-foreground">Disabled</p>
      )}

      {enabled && (
        <KVTable
          rows={[
            schedule && ["Schedule", schedule],
            imageFilter && ["Image filter", imageFilter],
            latest && ["Latest", "Always pull latest"],
            updateOpts && ["Update options", updateOpts],
          ]}
        />
      )}
    </IntegrationSection>
  );
}
