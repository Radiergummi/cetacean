import { IntegrationSection } from "./IntegrationSection";
import type { GantryIntegration } from "@/api/types";
import { KVTable } from "@/components/data";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { NumberField } from "@/components/ui/number-field";
import { Switch } from "@/components/ui/switch";
import { saveModelledLabels } from "@/lib/integrationLabels";
import { useState } from "react";

const docsUrl = "https://github.com/shizunge/gantry#labels";

const manifestCommands = ["buildx", "manifest", "none"] as const;

/**
 * A label Gantry reads as an override: "" leaves Gantry's own default in place.
 */
type Override = "" | "true" | "false";

interface GantryForm {
  excluded: boolean;
  authConfig: string;
  manifestCmd: string;
  manifestOptions: string;
  rollbackOnFailure: Override;
  rollbackOptions: string;
  updateJobs: Override;
  updateOptions: string;
  updateTimeoutSeconds: number | undefined;
}

function toOverride(value: boolean | undefined): Override {
  return value === undefined ? "" : value ? "true" : "false";
}

function formFrom(integration: GantryIntegration): GantryForm {
  return {
    excluded: !integration.enabled,
    authConfig: integration.authConfig ?? "",
    manifestCmd: integration.manifestCmd ?? "",
    manifestOptions: integration.manifestOptions ?? "",
    rollbackOnFailure: toOverride(integration.rollbackOnFailure),
    rollbackOptions: integration.rollbackOptions ?? "",
    updateJobs: toOverride(integration.updateJobs),
    updateOptions: integration.updateOptions ?? "",
    updateTimeoutSeconds: integration.updateTimeoutSeconds,
  };
}

function serializeToLabels(form: GantryForm): Record<string, string> {
  const labels: Record<string, string> = {};
  const text: [string, string][] = [
    ["gantry.auth.config", form.authConfig],
    ["gantry.manifest.cmd", form.manifestCmd],
    ["gantry.manifest.options", form.manifestOptions],
    ["gantry.rollback.on_failure", form.rollbackOnFailure],
    ["gantry.rollback.options", form.rollbackOptions],
    ["gantry.update.jobs", form.updateJobs],
    ["gantry.update.options", form.updateOptions],
  ];

  if (form.excluded) {
    labels["gantry.services.excluded"] = "true";
  }

  for (const [key, value] of text) {
    if (value.trim()) {
      labels[key] = value;
    }
  }

  if (form.updateTimeoutSeconds !== undefined) {
    labels["gantry.update.timeout_seconds"] = String(form.updateTimeoutSeconds);
  }

  return labels;
}

function OverrideSelect({
  label,
  description,
  value,
  onChange,
}: {
  label: string;
  description: string;
  value: Override;
  onChange: (value: Override) => void;
}) {
  return (
    <Field
      label={label}
      description={description}
    >
      {(control) => (
        <select
          {...control}
          className="h-8 w-full rounded-md border bg-background px-2 text-sm"
          value={value}
          onChange={(event) => onChange(event.target.value as Override)}
        >
          <option value="">Gantry default</option>
          <option value="true">Yes</option>
          <option value="false">No</option>
        </select>
      )}
    </Field>
  );
}

function yesNo(value: boolean | undefined): string | undefined {
  return value === undefined ? undefined : value ? "Yes" : "No";
}

/**
 * Panel displaying parsed Gantry service update configuration,
 * with optional inline editing support.
 */
export function GantryPanel({
  integration,
  rawLabels,
  serviceId,
  onSaved,
  editable,
}: {
  integration: GantryIntegration;
  rawLabels: [string, string][];
  serviceId: string;
  onSaved: (updated: Record<string, string>) => void;
  editable?: boolean | undefined;
}) {
  const [form, setForm] = useState(() => formFrom(integration));

  function update<Key extends keyof GantryForm>(key: Key, value: GantryForm[Key]) {
    setForm((previous) => ({ ...previous, [key]: value }));
  }

  async function handleSave(ifMatch: string | undefined) {
    await saveModelledLabels(
      rawLabels,
      serializeToLabels(formFrom(integration)),
      serializeToLabels(form),
      serviceId,
      onSaved,
      ifMatch,
    );
  }

  const editForm = (
    <div className="space-y-3">
      <div className="flex flex-col gap-1.5">
        <label className="flex items-center gap-2">
          <Switch
            checked={!form.excluded}
            onCheckedChange={(checked) => update("excluded", !checked)}
          />
          <span className="text-xs font-medium text-foreground">Enabled</span>
        </label>
        <p className="text-xs text-muted-foreground">
          Off sets <code>gantry.services.excluded</code>, which Gantry's default filter skips
        </p>
      </div>

      <div className="grid items-start gap-x-4 gap-y-3 lg:grid-cols-3">
        <Field
          label="Manifest command"
          description="How Gantry checks for a new image"
        >
          {(control) => (
            <select
              {...control}
              className="h-8 w-full rounded-md border bg-background px-2 text-sm"
              value={form.manifestCmd}
              onChange={(event) => update("manifestCmd", event.target.value)}
            >
              <option value="">Gantry default</option>
              {manifestCommands.map((command) => (
                <option
                  key={command}
                  value={command}
                >
                  {command}
                </option>
              ))}
            </select>
          )}
        </Field>

        <OverrideSelect
          label="Roll back on failure"
          description="Roll the service back when its update fails"
          value={form.rollbackOnFailure}
          onChange={(value) => update("rollbackOnFailure", value)}
        />

        <OverrideSelect
          label="Update jobs"
          description="Also update replicated and global jobs"
          value={form.updateJobs}
          onChange={(value) => update("updateJobs", value)}
        />

        <div className="flex flex-col gap-1.5">
          <NumberField
            label="Update timeout"
            value={form.updateTimeoutSeconds}
            onChange={(value) => update("updateTimeoutSeconds", value)}
            min={0}
            clearable
          />
          <p className="text-xs text-muted-foreground">Seconds per update (0 = no timeout)</p>
        </div>

        <Field
          label="Auth config"
          description="Docker config directory for this service"
        >
          {(control) => (
            <Input
              {...control}
              value={form.authConfig}
              onChange={(event) => update("authConfig", event.target.value)}
            />
          )}
        </Field>

        <Field
          label="Manifest options"
          description="Options for the manifest command"
        >
          {(control) => (
            <Input
              {...control}
              value={form.manifestOptions}
              onChange={(event) => update("manifestOptions", event.target.value)}
            />
          )}
        </Field>

        <Field
          label="Update options"
          description="Options for docker service update"
        >
          {(control) => (
            <Input
              {...control}
              value={form.updateOptions}
              onChange={(event) => update("updateOptions", event.target.value)}
            />
          )}
        </Field>

        <Field
          label="Rollback options"
          description="Options for the rollback"
        >
          {(control) => (
            <Input
              {...control}
              value={form.rollbackOptions}
              onChange={(event) => update("rollbackOptions", event.target.value)}
            />
          )}
        </Field>
      </div>
    </div>
  );

  return (
    <IntegrationSection
      title="Gantry"
      defaultOpen={integration.enabled}
      enabled={integration.enabled}
      rawLabels={rawLabels}
      docsUrl={docsUrl}
      editable={editable}
      editContent={editForm}
      onEditStart={() => setForm(formFrom(integration))}
      onSave={handleSave}
      serviceId={serviceId}
      onRawSave={onSaved}
    >
      <KVTable
        rows={[
          integration.manifestCmd && ["Manifest command", integration.manifestCmd],
          integration.manifestOptions && ["Manifest options", integration.manifestOptions],
          ["Roll back on failure", yesNo(integration.rollbackOnFailure)],
          integration.rollbackOptions && ["Rollback options", integration.rollbackOptions],
          ["Update jobs", yesNo(integration.updateJobs)],
          integration.updateOptions && ["Update options", integration.updateOptions],
          integration.updateTimeoutSeconds !== undefined && [
            "Update timeout",
            integration.updateTimeoutSeconds === 0
              ? "None"
              : `${integration.updateTimeoutSeconds}s`,
          ],
          integration.authConfig && ["Auth config", integration.authConfig],
        ]}
      />
    </IntegrationSection>
  );
}
