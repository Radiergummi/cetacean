import type { CronjobIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { IntegrationSection } from "./IntegrationSection";

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
  const { enabled, schedule, skipRunning, replicas } = integration;

  return (
    <IntegrationSection title="Swarm Cronjob" defaultOpen={enabled} rawLabels={rawLabels}>
      {!enabled && (
        <p className="text-sm text-muted-foreground">Disabled</p>
      )}

      {enabled && (
        <KVTable
          rows={[
            schedule && ["Schedule", schedule],
            replicas != null && replicas > 0 && ["Replicas", String(replicas)],
            skipRunning && ["Skip running", "Skip if previous run still active"],
          ]}
        />
      )}
    </IntegrationSection>
  );
}
