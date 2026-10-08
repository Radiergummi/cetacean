import { IntegrationSection } from "./IntegrationSection";
import type { AclIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { MultiCombobox } from "@/components/ui/multi-combobox";
import { saveIntegrationLabels } from "@/lib/integrationLabels";
import { useId, useState } from "react";

const labelKeyRead = "cetacean.acl.read";
const labelKeyWrite = "cetacean.acl.write";

/**
 * Panel displaying parsed ACL audience configuration,
 * with optional inline editing support.
 */
export function AclPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: AclIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean;
}) {
  const { read, write } = integration;
  const [formRead, setFormRead] = useState<string[]>(read ?? []);
  const [formWrite, setFormWrite] = useState<string[]>(write ?? []);
  const readId = useId();
  const writeId = useId();

  const fields = [
    { label: "Read", key: labelKeyRead, id: readId, values: formRead, onChange: setFormRead },
    { label: "Write", key: labelKeyWrite, id: writeId, values: formWrite, onChange: setFormWrite },
  ];

  function resetForm() {
    setFormRead(read ?? []);
    setFormWrite(write ?? []);
  }

  function serializeToLabels(): Record<string, string> {
    const labels: Record<string, string> = {};

    for (const { key, values } of fields) {
      const audiences = values.filter((audience) => audience.trim());

      if (audiences.length > 0) {
        labels[key] = audiences.join(",");
      }
    }

    return labels;
  }

  async function handleSave() {
    await saveIntegrationLabels(rawLabels, serializeToLabels(), serviceId, onSaved);
  }

  const editForm = (
    <div className="space-y-4">
      {fields.map(({ label, id, values, onChange }) => (
        <div
          key={id}
          className="flex flex-col gap-1.5"
        >
          <label
            htmlFor={id}
            className="text-xs font-medium text-foreground"
          >
            {label}
          </label>
          <MultiCombobox
            id={id}
            values={values}
            onChange={onChange}
            options={[]}
            placeholder="group:ops or user:alice@example.com"
          />
        </div>
      ))}
    </div>
  );

  return (
    <IntegrationSection
      title="Access Control"
      defaultOpen
      enabled
      rawLabels={rawLabels}
      editable={editable}
      editContent={editForm}
      onEditStart={resetForm}
      onSave={handleSave}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      <KVTable
        rows={[
          read && read.length > 0 && (["Read", read.join(", ")] as [string, string]),
          write && write.length > 0 && (["Write", write.join(", ")] as [string, string]),
        ]}
      />
    </IntegrationSection>
  );
}
