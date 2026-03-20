import type { HistoryEntry } from "../api/types";
import ActivityFeed from "./ActivityFeed";
import CollapsibleSection from "./CollapsibleSection";

export default function ActivitySection({
  entries,
  hideType,
}: {
  entries: HistoryEntry[];
  hideType?: boolean;
}) {
  if (entries.length === 0) {
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
