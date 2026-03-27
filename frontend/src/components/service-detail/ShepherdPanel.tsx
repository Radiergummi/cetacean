import type { ShepherdIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { CronSchedule } from "./CronSchedule";
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
  const { enabled, schedule, imageFilter, latest, updateOpts } = integration;

  return (
    <IntegrationSection title="Shepherd" defaultOpen={enabled} rawLabels={rawLabels} docsUrl={docsUrl}>
      {!enabled && (
        <p className="text-sm text-muted-foreground">Disabled</p>
      )}

      {enabled && (
        <KVTable
          rows={[
            schedule && ["Schedule", <CronSchedule key="schedule" expression={schedule} />],
            imageFilter && ["Image filter", imageFilter],
            latest && ["Latest", "Always pull latest"],
            updateOpts && ["Update options", updateOpts],
          ]}
        />
      )}
    </IntegrationSection>
  );
}
