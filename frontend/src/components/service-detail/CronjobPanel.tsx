import { CronSchedule } from "./CronSchedule";
import { IntegrationSection } from "./IntegrationSection";
import type { CronjobIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { Input } from "@/components/ui/input";
import { NumberField } from "@/components/ui/number-field";
import { Switch } from "@/components/ui/switch";
import { saveIntegrationLabels } from "@/lib/integrationLabels";
import { CronExpressionParser } from "cron-parser";
import { useState } from "react";

const docsUrl = "https://github.com/crazy-max/swarm-cronjob#usage";

function validateCron(expression: string): string | null {
  if (!expression.trim()) {
    return null;
  }

  try {
    CronExpressionParser.parse(expression);
    return null;
  } catch {
    return "Invalid cron expression";
  }
}

/**
 * Panel displaying parsed Swarm Cronjob configuration,
 * with optional inline editing support.
 */
export function CronjobPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: CronjobIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean;
}) {
  const { enabled, schedule, skipRunning, replicas, registryAuth, queryRegistry } = integration;

  const [formEnabled, setFormEnabled] = useState(integration.enabled);
  const [formSchedule, setFormSchedule] = useState(integration.schedule ?? "");
  const [formSkipRunning, setFormSkipRunning] = useState(integration.skipRunning ?? false);
  const [formReplicas, setFormReplicas] = useState(integration.replicas ?? 1);
  const [formRegistryAuth, setFormRegistryAuth] = useState(integration.registryAuth ?? false);
  const [formQueryRegistry, setFormQueryRegistry] = useState(integration.queryRegistry ?? false);

  const cronError = validateCron(formSchedule);

  function resetForm() {
    setFormEnabled(integration.enabled);
    setFormSchedule(integration.schedule ?? "");
    setFormSkipRunning(integration.skipRunning ?? false);
    setFormReplicas(integration.replicas ?? 1);
    setFormRegistryAuth(integration.registryAuth ?? false);
    setFormQueryRegistry(integration.queryRegistry ?? false);
  }

  function serializeToLabels(): Record<string, string> {
    const labels: Record<string, string> = {
      "swarm.cronjob.enable": String(formEnabled),
    };

    if (formSchedule.trim()) {
      labels["swarm.cronjob.schedule"] = formSchedule;
    }

    if (formSkipRunning) {
      labels["swarm.cronjob.skip-running"] = "true";
    }

    if (formReplicas > 1) {
      labels["swarm.cronjob.replicas"] = String(formReplicas);
    }

    if (formRegistryAuth) {
      labels["swarm.cronjob.registry-auth"] = "true";
    }

    if (formQueryRegistry) {
      labels["swarm.cronjob.query-registry"] = "true";
    }

    return labels;
  }

  async function handleSave() {
    if (cronError) {
      throw new Error(cronError);
    }

    await saveIntegrationLabels(rawLabels, serializeToLabels(), serviceId, onSaved);
  }

  const editForm = (
    <div className="space-y-3">
      <label className="flex items-center gap-2">
        <Switch
          checked={formEnabled}
          onCheckedChange={setFormEnabled}
        />
        <span className="text-xs font-medium text-foreground">Enabled</span>
      </label>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground">Schedule</label>
        <Input
          className="font-mono"
          value={formSchedule}
          onChange={(event) => setFormSchedule(event.target.value)}
          placeholder="*/5 * * * *"
        />
        {cronError && <p className="text-xs text-destructive">{cronError}</p>}
      </div>

      <label className="flex items-center gap-2">
        <Switch
          checked={formSkipRunning}
          onCheckedChange={setFormSkipRunning}
        />
        <span className="text-xs font-medium text-foreground">Skip running</span>
      </label>

      <div className="flex flex-col gap-1.5">
        <NumberField
          label="Replicas"
          value={formReplicas}
          onChange={(value) => setFormReplicas(value ?? 1)}
          min={1}
        />
      </div>

      <label className="flex items-center gap-2">
        <Switch
          checked={formRegistryAuth}
          onCheckedChange={setFormRegistryAuth}
        />
        <span className="text-xs font-medium text-foreground">Registry auth</span>
      </label>

      <label className="flex items-center gap-2">
        <Switch
          checked={formQueryRegistry}
          onCheckedChange={setFormQueryRegistry}
        />
        <span className="text-xs font-medium text-foreground">Query registry</span>
      </label>
    </div>
  );

  return (
    <IntegrationSection
      title="Swarm Cronjob"
      defaultOpen={enabled}
      enabled={enabled}
      rawLabels={rawLabels}
      docsUrl={docsUrl}
      editable={editable}
      editContent={editForm}
      onEditStart={resetForm}
      onSave={handleSave}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      <KVTable
        rows={[
          schedule && [
            "Schedule",
            <CronSchedule
              key="schedule"
              expression={schedule}
            />,
          ],
          replicas != null && replicas > 0 && ["Replicas", String(replicas)],
          skipRunning && ["Skip running", "Skip if previous run still active"],
          registryAuth && ["Registry auth", "Enabled"],
          queryRegistry && ["Query registry", "Enabled"],
        ]}
      />
    </IntegrationSection>
  );
}
