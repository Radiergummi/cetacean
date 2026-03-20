import { api } from "@/api/client";
import type { Healthcheck } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { SliderNumberField } from "@/components/ui/slider-number-field";
import { formatDuration } from "@/lib/format";
import { joinCommand, parseCommand } from "@/lib/parseCommand";
import { cn, getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { useState } from "react";

interface FormState {
  enabled: boolean;
  useShell: boolean;
  command: string;
  interval: number | undefined;
  timeout: number | undefined;
  startPeriod: number | undefined;
  startInterval: number | undefined;
  retries: number | undefined;
}

const emptyForm: FormState = {
  enabled: true,
  useShell: true,
  command: "",
  interval: undefined,
  timeout: undefined,
  startPeriod: undefined,
  startInterval: undefined,
  retries: undefined,
};

function nanosToSeconds(nanoseconds: number | undefined): number | undefined {
  if (!nanoseconds) {
    return undefined;
  }

  return nanoseconds / 1e9;
}

function formatHealthcheckDuration(nanoseconds: number | undefined): string {
  if (nanoseconds == null || nanoseconds === 0) {
    return "default";
  }

  return formatDuration(nanoseconds);
}

function formatRetries(retries: number | undefined): string {
  if (retries == null || retries === 0) {
    return "default";
  }

  return String(retries);
}

function extractCommand(test: string[] | undefined): { shell: boolean; command: string } {
  if (!test || test.length === 0) {
    return { shell: true, command: "" };
  }

  if (test[0] === "CMD-SHELL") {
    return { shell: true, command: test[1] ?? "" };
  }

  if (test[0] === "CMD") {
    return { shell: false, command: joinCommand(test.slice(1)) };
  }

  return { shell: true, command: joinCommand(test) };
}

function isDisabled(healthcheck: Healthcheck | null): boolean {
  if (!healthcheck) {
    return true;
  }

  return healthcheck.Test?.[0] === "NONE";
}

function formFromHealthcheck(healthcheck: Healthcheck | null): FormState {
  const disabled = isDisabled(healthcheck);

  if (disabled || !healthcheck) {
    return { ...emptyForm, enabled: false };
  }

  const extracted = extractCommand(healthcheck.Test);

  return {
    enabled: true,
    useShell: extracted.shell,
    command: extracted.command,
    interval: nanosToSeconds(healthcheck.Interval),
    timeout: nanosToSeconds(healthcheck.Timeout),
    startPeriod: nanosToSeconds(healthcheck.StartPeriod),
    startInterval: nanosToSeconds(healthcheck.StartInterval),
    retries: healthcheck.Retries || undefined,
  };
}

function formToHealthcheck(form: FormState): Healthcheck {
  let test: string[];

  if (!form.enabled) {
    test = ["NONE"];
  } else if (form.useShell) {
    test = ["CMD-SHELL", form.command];
  } else {
    test = ["CMD", ...parseCommand(form.command)];
  }

  return {
    Test: test,
    Interval: form.interval != null ? form.interval * 1e9 : 0,
    Timeout: form.timeout != null ? form.timeout * 1e9 : 0,
    StartPeriod: form.startPeriod != null ? form.startPeriod * 1e9 : 0,
    StartInterval: form.startInterval != null ? form.startInterval * 1e9 : 0,
    Retries: form.retries ?? 0,
  };
}

export function HealthcheckEditor({
  serviceId,
  healthcheck,
  onSaved,
}: {
  serviceId: string;
  healthcheck: Healthcheck | null;
  onSaved: (updated: Healthcheck | null) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(emptyForm);

  function updateForm(partial: Partial<FormState>) {
    setForm((previous) => ({ ...previous, ...partial }));
  }

  function openEdit() {
    setForm(formFromHealthcheck(healthcheck));
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  async function save() {
    setSaving(true);
    setSaveError(null);

    try {
      const result = await api.putServiceHealthcheck(serviceId, formToHealthcheck(form));
      onSaved(result.healthcheck);
      setEditing(false);
    } catch (error) {
      setSaveError(getErrorMessage(error, "Save failed"));
    } finally {
      setSaving(false);
    }
  }

  const disabled = isDisabled(healthcheck);
  const extracted = !disabled && healthcheck ? extractCommand(healthcheck.Test) : null;

  const controls = !editing ? (
    <Button
      variant="outline"
      size="xs"
      onClick={(event: React.MouseEvent) => {
        event.stopPropagation();
        openEdit();
      }}
    >
      <Pencil className="size-3" />
      Edit
    </Button>
  ) : undefined;

  return (
    <CollapsibleSection
      title="Healthcheck"
      defaultOpen={!disabled}
      controls={controls}
    >
      <div className="rounded-lg border p-3">
        {editing ? (
          <EditMode
            form={form}
            updateForm={updateForm}
            saving={saving}
            saveError={saveError}
            onSave={() => void save()}
            onCancel={cancelEdit}
          />
        ) : disabled ? (
          <p className="text-sm text-muted-foreground">No healthcheck configured</p>
        ) : (
          <DisplayMode
            healthcheck={healthcheck!}
            shell={extracted!.shell}
            command={extracted!.command}
          />
        )}
      </div>
    </CollapsibleSection>
  );
}

function DisplayMode({
  healthcheck,
  shell,
  command,
}: {
  healthcheck: Healthcheck;
  shell: boolean;
  command: string;
}) {
  return (
    <div className="space-y-3">
      {/* Command row */}
      <div className="flex items-start gap-2">
        <span
          data-mode={shell ? "shell" : "exec"}
          className="mt-0.5 shrink-0 rounded px-1.5 py-0.5 text-xs font-medium data-[mode=exec]:bg-blue-100 data-[mode=exec]:text-blue-800 data-[mode=shell]:bg-green-100 data-[mode=shell]:text-green-800 dark:data-[mode=exec]:bg-blue-900/30 dark:data-[mode=exec]:text-blue-300 dark:data-[mode=shell]:bg-green-900/30 dark:data-[mode=shell]:text-green-300"
        >
          {shell ? "Shell" : "Exec"}
        </span>
        <code className="min-w-0 flex-1 rounded bg-muted px-2 py-1 font-mono text-xs break-all">
          {command}
        </code>
        <CopyButton text={command} />
      </div>

      {/* Stat cards */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard
          label="Interval"
          value={formatHealthcheckDuration(healthcheck.Interval)}
        />
        <StatCard
          label="Timeout"
          value={formatHealthcheckDuration(healthcheck.Timeout)}
        />
        <StatCard
          label="Start Period"
          value={formatHealthcheckDuration(healthcheck.StartPeriod)}
        />
        {!!healthcheck.StartInterval && (
          <StatCard
            label="Start Interval"
            value={formatHealthcheckDuration(healthcheck.StartInterval)}
          />
        )}
        <StatCard
          label="Retries"
          value={formatRetries(healthcheck.Retries)}
        />
      </div>
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="font-mono text-sm">{value}</div>
    </div>
  );
}

function ToggleButton({
  label,
  pressed,
  onToggle,
  className,
}: {
  label: string;
  pressed: boolean;
  onToggle: (value: boolean) => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={pressed}
      onClick={() => onToggle(!pressed)}
      className={cn("inline-flex items-center gap-2 text-sm", className)}
    >
      <span
        data-on={pressed || undefined}
        className="relative inline-block h-5 w-9 rounded-full bg-muted transition-colors data-[on]:bg-primary"
      >
        <span
          data-on={pressed || undefined}
          className="absolute top-0.5 left-0.5 size-4 rounded-full bg-background shadow transition-transform data-[on]:translate-x-4"
        />
      </span>
      {label}
    </button>
  );
}

function EditMode({
  form,
  updateForm,
  saving,
  saveError,
  onSave,
  onCancel,
}: {
  form: FormState;
  updateForm: (partial: Partial<FormState>) => void;
  saving: boolean;
  saveError: string | null;
  onSave: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-4">
        <ToggleButton
          label="Enabled"
          pressed={form.enabled}
          onToggle={(enabled) => updateForm({ enabled })}
        />

        {form.enabled && (
          <ToggleButton
            label="Use Shell"
            pressed={form.useShell}
            onToggle={(useShell) => updateForm({ useShell })}
            className="ms-auto sm:hidden"
          />
        )}
      </div>

      {form.enabled && (
        <>
          <div className="flex flex-col gap-1">
            <div className="flex items-center">
              <label className="text-xs text-muted-foreground">Command</label>
              <ToggleButton
                label="Use Shell"
                pressed={form.useShell}
                onToggle={(useShell) => updateForm({ useShell })}
                className="ms-auto hidden sm:inline-flex"
              />
            </div>
            <input
              type="text"
              value={form.command}
              onChange={(event) => updateForm({ command: event.target.value })}
              placeholder={form.useShell ? "curl -f http://localhost/ || exit 1" : "/bin/healthcheck"}
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
            />
            {!form.useShell && (
              <p className="text-xs text-muted-foreground">Executed directly, not via shell</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <SliderNumberField
              label="Interval (s)"
              value={form.interval}
              onChange={(interval) => updateForm({ interval })}
              min={0}
              step={0.1}
            />
            <SliderNumberField
              label="Timeout (s)"
              value={form.timeout}
              onChange={(timeout) => updateForm({ timeout })}
              min={0}
              step={0.1}
            />
            <SliderNumberField
              label="Start Period (s)"
              value={form.startPeriod}
              onChange={(startPeriod) => updateForm({ startPeriod })}
              min={0}
              step={0.1}
            />
            <SliderNumberField
              label="Start Interval (s)"
              value={form.startInterval}
              onChange={(startInterval) => updateForm({ startInterval })}
              min={0}
              step={0.1}
            />
          </div>

          <div className="w-48">
            <SliderNumberField
              label="Retries"
              value={form.retries}
              onChange={(retries) => updateForm({ retries })}
              min={0}
              step={1}
            />
          </div>
        </>
      )}

      {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

      <footer className="flex items-center justify-end gap-2">
        <Button
          size="sm"
          onClick={onSave}
          disabled={saving}
        >
          {saving && <Spinner className="size-3" />}
          Save
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={onCancel}
          disabled={saving}
        >
          Cancel
        </Button>
      </footer>
    </div>
  );
}
