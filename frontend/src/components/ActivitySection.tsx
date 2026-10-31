import type { HistoryEntry } from "../api/types";
import { hasChanges } from "../lib/activity";
import ActivityFeed from "./ActivityFeed";
import CollapsibleSection from "./CollapsibleSection";

export default function ActivitySection({
  entries,
  hideType,
}: {
  entries: HistoryEntry[];
  hideType?: boolean | undefined;
}) {
  if (!hasChanges(entries)) {
    return null;
  }

  return (
    <CollapsibleSection title="Recent Activity">
      <ActivityFeed
        entries={entries}
        hideType={hideType}
      />
    </CollapsibleSection>
  );
}
