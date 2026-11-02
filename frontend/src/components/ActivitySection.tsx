import type { AnnotationScope, HistoryEntry } from "../api/types";
import { hasActivity } from "../lib/activity";
import ActivityFeed from "./ActivityFeed";
import AddAnnotationButton from "./annotations/AddAnnotationButton";
import CollapsibleSection from "./CollapsibleSection";

export default function ActivitySection({
  entries,
  hideType,
  annotate,
}: {
  entries: HistoryEntry[];
  hideType?: boolean | undefined;

  /** The resource a note added here is about; set when the caller may annotate it. */
  annotate?: Exclude<AnnotationScope, "cluster"> | undefined;
}) {
  if (!hasActivity(entries) && !annotate) {
    return null;
  }

  return (
    <CollapsibleSection
      title="Recent Activity"
      controls={annotate && <AddAnnotationButton scope={annotate} />}
    >
      <ActivityFeed
        entries={entries}
        hideType={hideType}
      />
    </CollapsibleSection>
  );
}
