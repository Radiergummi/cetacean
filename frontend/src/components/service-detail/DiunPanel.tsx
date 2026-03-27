import type { DiunIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import KeyValuePills from "@/components/data/KeyValuePills";
import { IntegrationSection } from "./IntegrationSection";

/**
 * Read-only panel displaying parsed Diun image update notifier configuration.
 */
export function DiunPanel({
  integration,
  rawLabels,
}: {
  integration: DiunIntegration;
  rawLabels: [string, string][];
}) {
  const { enabled, watchRepo, notifyOn, maxTags, includeTags, excludeTags, sortTags, metadata } =
    integration;

  const hasMetadata = metadata && Object.keys(metadata).length > 0;

  return (
    <IntegrationSection title="Diun" defaultOpen={enabled} rawLabels={rawLabels}>
      {!enabled && (
        <p className="text-sm text-muted-foreground">Disabled</p>
      )}

      {enabled && (
        <div className="flex flex-col gap-3">
          <KVTable
            rows={[
              watchRepo && ["Watch repo", "Entire repository"],
              notifyOn && ["Notify on", notifyOn],
              maxTags != null && maxTags > 0 && ["Max tags", String(maxTags)],
              includeTags && ["Include tags", includeTags],
              excludeTags && ["Exclude tags", excludeTags],
              sortTags && ["Sort tags", sortTags],
            ]}
          />

          {hasMetadata && (
            <div className="flex flex-col gap-1.5">
              <div className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Metadata
              </div>
              <KeyValuePills entries={Object.entries(metadata)} />
            </div>
          )}
        </div>
      )}
    </IntegrationSection>
  );
}
