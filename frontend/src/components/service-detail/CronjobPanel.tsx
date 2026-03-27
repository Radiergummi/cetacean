import type { CronjobIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { CronSchedule } from "./CronSchedule";
import { IntegrationSection } from "./IntegrationSection";

const docsUrl = "https://github.com/crazy-max/swarm-cronjob#usage";

/**
 * Read-only panel displaying parsed Swarm Cronjob configuration.
 */
export function CronjobPanel({
  integration,
  rawLabels,
}: {
  integration: CronjobIntegration;
  rawLabels: [string, string][];
}) {
  const { enabled, schedule, skipRunning, replicas, registryAuth, queryRegistry } = integration;

  return (
    <IntegrationSection title="Swarm Cronjob" defaultOpen={enabled} rawLabels={rawLabels} docsUrl={docsUrl}>
      {!enabled && (
        <p className="text-sm text-muted-foreground">Disabled</p>
      )}

      {enabled && (
        <KVTable
          rows={[
            schedule && ["Schedule", <CronSchedule key="schedule" expression={schedule} />],
            replicas != null && replicas > 0 && ["Replicas", String(replicas)],
            skipRunning && ["Skip running", "Skip if previous run still active"],
            registryAuth && ["Registry auth", "Enabled"],
            queryRegistry && ["Query registry", "Enabled"],
          ]}
        />
      )}
    </IntegrationSection>
  );
}
