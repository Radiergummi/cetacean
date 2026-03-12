import type { HistoryEntry } from "../api/types";
import ActivityFeed from "./ActivityFeed";
import { SectionHeader } from "./data";

export default function ActivitySection({ entries }: { entries: HistoryEntry[] }) {
  if (entries.length === 0) return null;
  return (
    <div>
      <SectionHeader title="Recent Activity" />
      <ActivityFeed entries={entries} />
    </div>
  );
}
