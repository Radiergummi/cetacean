import CollapsibleSection from "../CollapsibleSection";
import KeyValuePills from "./KeyValuePills";

export default function LabelSection({ entries }: { entries: [string, string][] }) {
  if (entries.length === 0) {
    return null;
  }

  return (
    <CollapsibleSection title="Labels">
      <KeyValuePills entries={entries} />
    </CollapsibleSection>
  );
}
