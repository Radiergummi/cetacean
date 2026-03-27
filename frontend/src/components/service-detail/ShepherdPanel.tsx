import { api } from "@/api/client";
import type { ShepherdIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { diffLabels } from "@/lib/integrationLabels";
import { useState } from "react";
import { IntegrationSection } from "./IntegrationSection";

const docsUrl = "https://github.com/djmaze/shepherd#usage";

/**
 * Panel displaying parsed Shepherd auto-update configuration,
 * with optional inline editing support.
 */
export function ShepherdPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: ShepherdIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean;
}) {
  const { enabled, authConfig } = integration;

  const [formEnabled, setFormEnabled] = useState(false);
  const [formAuthConfig, setFormAuthConfig] = useState("");

  function resetForm() {
    setFormEnabled(integration.enabled);
    setFormAuthConfig(integration.authConfig ?? "");
  }

  function serializeToLabels(): Record<string, string> {
    const labels: Record<string, string> = {
      "shepherd.enable": String(formEnabled),
    };

    if (formAuthConfig.trim() !== "") {
      labels["shepherd.auth.config"] = formAuthConfig;
    }

    return labels;
  }

  async function handleSave() {
    const ops = diffLabels(rawLabels, serializeToLabels());
    const updated = await api.patchServiceLabels(serviceId, ops);
    onSaved(updated);
  }

  const editForm = (
    <div className="space-y-3">
      <label className="flex items-center gap-2">
        <Switch checked={formEnabled} onCheckedChange={setFormEnabled} />
        <span className="text-xs font-medium text-foreground">Enabled</span>
      </label>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Auth config</label>
        <Input
          value={formAuthConfig}
          onChange={(event) => setFormAuthConfig(event.target.value)}
          placeholder="registry:credentials"
        />
      </div>
    </div>
  );

  return (
    <IntegrationSection
      title="Shepherd"
      defaultOpen={enabled}
      rawLabels={rawLabels}
      docsUrl={docsUrl}
      editable={editable}
      editContent={editForm}
      onEditStart={resetForm}
      onSave={handleSave}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      {!enabled && (
        <p className="text-sm text-muted-foreground">Disabled</p>
      )}

      {enabled && (
        <KVTable rows={[authConfig && ["Auth config", authConfig]]} />
      )}
    </IntegrationSection>
  );
}
