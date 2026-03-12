import KeyValuePills from "./KeyValuePills";
import SectionHeader from "./SectionHeader";

export default function LabelSection({ entries }: { entries: [string, string][] }) {
  if (entries.length === 0) return null;
  return (
    <div>
      <SectionHeader title="Labels" />
      <KeyValuePills entries={entries} />
    </div>
  );
}
