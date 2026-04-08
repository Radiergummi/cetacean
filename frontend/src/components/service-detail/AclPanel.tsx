import { IntegrationSection } from "./IntegrationSection";
import type { AclIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { MultiCombobox } from "@/components/ui/multi-combobox";
import { saveIntegrationLabels } from "@/lib/integrationLabels";
import { useState } from "react";

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

  function resetForm() {
    setFormRead(integration.read ?? []);
    setFormWrite(integration.write ?? []);
  }

  function serializeToLabels(): Record<string, string> {
    const labels: Record<string, string> = {};
    const readFiltered = formRead.filter((audience) => audience.trim());
    const writeFiltered = formWrite.filter((audience) => audience.trim());

    if (readFiltered.length > 0) {
      labels["cetacean.acl.read"] = readFiltered.join(",");
    }

    if (writeFiltered.length > 0) {
      labels["cetacean.acl.write"] = writeFiltered.join(",");
    }

    return labels;
  }

  async function handleSave() {
    await saveIntegrationLabels(rawLabels, serializeToLabels(), serviceId, onSaved);
  }

  const editForm = (
    <div className="space-y-4">
      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Read</label>
        <MultiCombobox
          values={formRead}
          onChange={setFormRead}
          options={[]}
          placeholder="group:ops or user:alice@example.com"
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Write</label>
        <MultiCombobox
          values={formWrite}
          onChange={setFormWrite}
          options={[]}
          placeholder="group:ops or user:alice@example.com"
        />
      </div>
    </div>
  );

  const rows = [
    read && read.length > 0 && (["Read", read.join(", ")] as [string, string]),
    write && write.length > 0 && (["Write", write.join(", ")] as [string, string]),
  ];

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
      <KVTable rows={rows} />
    </IntegrationSection>
  );
}
