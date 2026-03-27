import { api } from "@/api/client";
import type { CronjobIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { diffLabels } from "@/lib/integrationLabels";
import { CronExpressionParser } from "cron-parser";
import { useState } from "react";
import { CronSchedule } from "./CronSchedule";
import { IntegrationSection } from "./IntegrationSection";

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

  const [formEnabled, setFormEnabled] = useState(true);
  const [formSchedule, setFormSchedule] = useState("");
  const [formSkipRunning, setFormSkipRunning] = useState(false);
  const [formReplicas, setFormReplicas] = useState(1);
  const [formRegistryAuth, setFormRegistryAuth] = useState(false);
  const [formQueryRegistry, setFormQueryRegistry] = useState(false);

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

    const ops = diffLabels(rawLabels, serializeToLabels());
    const updated = await api.patchServiceLabels(serviceId, ops);
    onSaved(updated);
  }

  const editForm = (
    <div className="flex flex-col gap-3">
      <label className="flex items-center justify-between">
        <span className="text-sm">Enabled</span>
        <Switch checked={formEnabled} onCheckedChange={setFormEnabled} />
      </label>

      <label className="flex items-center justify-between gap-4">
        <span className="text-sm">Schedule</span>
        <div className="flex flex-col items-end gap-1">
          <Input
            className="w-64 font-mono"
            value={formSchedule}
            onChange={(event) => setFormSchedule(event.target.value)}
            placeholder="*/5 * * * *"
          />
          {cronError && <span className="text-xs text-destructive">{cronError}</span>}
        </div>
      </label>

      <label className="flex items-center justify-between">
        <span className="text-sm">Skip running</span>
        <Switch checked={formSkipRunning} onCheckedChange={setFormSkipRunning} />
      </label>

      <label className="flex items-center justify-between gap-4">
        <span className="text-sm">Replicas</span>
        <Input
          type="number"
          className="w-24"
          min={1}
          value={formReplicas}
          onChange={(event) => setFormReplicas(Number(event.target.value))}
        />
      </label>

      <label className="flex items-center justify-between">
        <span className="text-sm">Registry auth</span>
        <Switch checked={formRegistryAuth} onCheckedChange={setFormRegistryAuth} />
      </label>

      <label className="flex items-center justify-between">
        <span className="text-sm">Query registry</span>
        <Switch checked={formQueryRegistry} onCheckedChange={setFormQueryRegistry} />
      </label>
    </div>
  );

  return (
    <IntegrationSection
      title="Swarm Cronjob"
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
        <KVTable
          rows={[
            schedule && ["Schedule", <CronSchedule key="schedule" expression={schedule} />],
            replicas != null && replicas > 0 && ["Replicas", String(replicas)],
            skipRunning && ["Skip running", "Skip if previous run still active"],
            registryAuth && ["Registry auth", "Enabled"],
            queryRegistry && ["Query registry", "Enabled"],
          ]}
        />
      )}
    </IntegrationSection>
  );
}
