import { IntegrationSection } from "./IntegrationSection";
import type { AclIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { saveIntegrationLabels } from "@/lib/integrationLabels";
import { useState } from "react";
import { X, Plus } from "lucide-react";

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
      <AudienceListEditor
        label="Read"
        audiences={formRead}
        onChange={setFormRead}
      />
      <AudienceListEditor
        label="Write"
        audiences={formWrite}
        onChange={setFormWrite}
      />
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

function AudienceListEditor({
  label,
  audiences,
  onChange,
}: {
  label: string;
  audiences: string[];
  onChange: (audiences: string[]) => void;
}) {
  function addEntry() {
    onChange([...audiences, ""]);
  }

  function removeEntry(index: number) {
    onChange(audiences.filter((_, entryIndex) => entryIndex !== index));
  }

  function updateEntry(index: number, value: string) {
    const updated = [...audiences];
    updated[index] = value;
    onChange(updated);
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between">
        <label className="text-xs font-medium text-foreground">{label}</label>
        <Button
          variant="ghost"
          size="sm"
          className="h-6 px-2 text-xs"
          onClick={addEntry}
        >
          <Plus className="mr-1 h-3 w-3" />
          Add
        </Button>
      </div>
      {audiences.map((audience, index) => (
        <div key={index} className="flex items-center gap-1.5">
          <Input
            value={audience}
            onChange={(event) => updateEntry(index, event.target.value)}
            placeholder="group:ops or user:alice@example.com"
            className="font-mono text-xs"
          />
          <Button
            variant="ghost"
            size="sm"
            className="h-8 w-8 shrink-0 p-0"
            onClick={() => removeEntry(index)}
          >
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      ))}
      {audiences.length === 0 && (
        <p className="text-xs text-muted-foreground">No audiences configured</p>
      )}
    </div>
  );
}
