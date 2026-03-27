import type { DiunIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import KeyValuePills from "@/components/data/KeyValuePills";
import { IntegrationSection } from "./IntegrationSection";

const docsUrl = "https://crazymax.dev/diun/providers/swarm/#docker-labels";

const badgeBase = "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium";
const badgeBlue = `${badgeBase} bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300`;

const notifyOnLabels: Record<string, string> = {
  new: "New image",
  update: "Updated tag",
};

function NotifyOnBadges({ value }: { value: string }) {
  const triggers = value.split(";").map((trigger) => trigger.trim()).filter(Boolean);

  return (
    <span className="inline-flex flex-wrap gap-1.5">
      {triggers.map((trigger) => (
        <span key={trigger} className={badgeBlue}>
          {notifyOnLabels[trigger] ?? trigger}
        </span>
      ))}
    </span>
  );
}

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
    <IntegrationSection title="Diun" defaultOpen={enabled} rawLabels={rawLabels} docsUrl={docsUrl}>
      {!enabled && (
        <p className="text-sm text-muted-foreground">Disabled</p>
      )}

      {enabled && (
        <div className="flex flex-col gap-3">
          <KVTable
            rows={[
              watchRepo && ["Watch repo", "Entire repository"],
              notifyOn && ["Notify on", <NotifyOnBadges key="notify-on" value={notifyOn} />],
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
