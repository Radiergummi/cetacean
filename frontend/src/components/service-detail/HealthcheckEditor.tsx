import { api } from "@/api/client";
import type { Healthcheck } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { joinCommand, parseCommand } from "@/lib/parseCommand";
import { getErrorMessage } from "@/lib/utils";
import { Check, Copy, Pencil } from "lucide-react";
import { useState } from "react";

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  function handleCopy() {
    navigator.clipboard.writeText(text).then(
      () => {
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      },
      () => {},
    );
  }

  return (
    <button
      type="button"
      onClick={handleCopy}
      className="shrink-0 cursor-pointer rounded p-1 text-muted-foreground/50 hover:text-muted-foreground"
    >
      {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
    </button>
  );
}

/**
 * Format nanoseconds as a human-readable seconds string.
 * Returns "default" for zero/undefined values.
 */
function formatNanoseconds(nanoseconds: number | undefined): string {
  if (!nanoseconds) {
    return "default";
  }

  return `${nanoseconds / 1e9}s`;
}

function formatRetries(retries: number | undefined): string {
  if (!retries) {
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

  // Edit mode state
  const [enabled, setEnabled] = useState(true);
  const [useShell, setUseShell] = useState(true);
  const [command, setCommand] = useState("");
  const [interval, setInterval] = useState("");
  const [timeout, setTimeout] = useState("");
  const [startPeriod, setStartPeriod] = useState("");
  const [startInterval, setStartInterval] = useState("");
  const [retries, setRetries] = useState("");

  function openEdit() {
    const disabled = isDisabled(healthcheck);
    setEnabled(!disabled);

    if (!disabled && healthcheck) {
      const extracted = extractCommand(healthcheck.Test);
      setUseShell(extracted.shell);
      setCommand(extracted.command);
      setInterval(healthcheck.Interval ? String(healthcheck.Interval / 1e9) : "");
      setTimeout(healthcheck.Timeout ? String(healthcheck.Timeout / 1e9) : "");
      setStartPeriod(healthcheck.StartPeriod ? String(healthcheck.StartPeriod / 1e9) : "");
      setStartInterval(healthcheck.StartInterval ? String(healthcheck.StartInterval / 1e9) : "");
      setRetries(healthcheck.Retries ? String(healthcheck.Retries) : "");
    } else {
      setUseShell(true);
      setCommand("");
      setInterval("");
      setTimeout("");
      setStartPeriod("");
      setStartInterval("");
      setRetries("");
    }

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
      let test: string[];

      if (!enabled) {
        test = ["NONE"];
      } else if (useShell) {
        test = ["CMD-SHELL", command];
      } else {
        test = ["CMD", ...parseCommand(command)];
      }

      const config: Healthcheck = {
        Test: test,
        Interval: interval ? parseFloat(interval) * 1e9 : 0,
        Timeout: timeout ? parseFloat(timeout) * 1e9 : 0,
        StartPeriod: startPeriod ? parseFloat(startPeriod) * 1e9 : 0,
        StartInterval: startInterval ? parseFloat(startInterval) * 1e9 : 0,
        Retries: retries ? parseInt(retries, 10) : 0,
      };

      const result = await api.putServiceHealthcheck(serviceId, config);
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
      {editing ? (
        <EditMode
          enabled={enabled}
          setEnabled={setEnabled}
          useShell={useShell}
          setUseShell={setUseShell}
          command={command}
          setCommand={setCommand}
          interval={interval}
          setInterval={setInterval}
          timeout={timeout}
          setTimeout={setTimeout}
          startPeriod={startPeriod}
          setStartPeriod={setStartPeriod}
          startInterval={startInterval}
          setStartInterval={setStartInterval}
          retries={retries}
          setRetries={setRetries}
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
          value={formatNanoseconds(healthcheck.Interval)}
        />
        <StatCard
          label="Timeout"
          value={formatNanoseconds(healthcheck.Timeout)}
        />
        <StatCard
          label="Start Period"
          value={formatNanoseconds(healthcheck.StartPeriod)}
        />
        {!!healthcheck.StartInterval && (
          <StatCard
            label="Start Interval"
            value={formatNanoseconds(healthcheck.StartInterval)}
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
}: {
  label: string;
  pressed: boolean;
  onToggle: (value: boolean) => void;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={pressed}
      onClick={() => onToggle(!pressed)}
      className="inline-flex items-center gap-2 text-sm"
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

function DurationInput({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-muted-foreground">
        {label} <span className="text-muted-foreground/60">(s)</span>
      </label>
      <input
        type="number"
        min="0"
        step="any"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder="default"
        className="h-8 rounded-md border bg-background px-2 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
      />
    </div>
  );
}

function EditMode({
  enabled,
  setEnabled,
  useShell,
  setUseShell,
  command,
  setCommand,
  interval,
  setInterval,
  timeout,
  setTimeout,
  startPeriod,
  setStartPeriod,
  startInterval,
  setStartInterval,
  retries,
  setRetries,
  saving,
  saveError,
  onSave,
  onCancel,
}: {
  enabled: boolean;
  setEnabled: (value: boolean) => void;
  useShell: boolean;
  setUseShell: (value: boolean) => void;
  command: string;
  setCommand: (value: string) => void;
  interval: string;
  setInterval: (value: string) => void;
  timeout: string;
  setTimeout: (value: string) => void;
  startPeriod: string;
  setStartPeriod: (value: string) => void;
  startInterval: string;
  setStartInterval: (value: string) => void;
  retries: string;
  setRetries: (value: string) => void;
  saving: boolean;
  saveError: string | null;
  onSave: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="space-y-4">
      <ToggleButton
        label="Enabled"
        pressed={enabled}
        onToggle={setEnabled}
      />

      {enabled && (
        <>
          <ToggleButton
            label="Use Shell"
            pressed={useShell}
            onToggle={setUseShell}
          />

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Command</label>
            <input
              type="text"
              value={command}
              onChange={(event) => setCommand(event.target.value)}
              placeholder={useShell ? "curl -f http://localhost/ || exit 1" : "/bin/healthcheck"}
              className="h-8 w-full rounded-md border bg-background px-2 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
            />
            {!useShell && (
              <p className="text-xs text-muted-foreground">Executed directly, not via shell</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <DurationInput
              label="Interval"
              value={interval}
              onChange={setInterval}
            />
            <DurationInput
              label="Timeout"
              value={timeout}
              onChange={setTimeout}
            />
            <DurationInput
              label="Start Period"
              value={startPeriod}
              onChange={setStartPeriod}
            />
            <DurationInput
              label="Start Interval"
              value={startInterval}
              onChange={setStartInterval}
            />
          </div>

          <div className="flex flex-col gap-1">
            <label className="text-xs text-muted-foreground">Retries</label>
            <input
              type="number"
              min="0"
              step="1"
              value={retries}
              onChange={(event) => setRetries(event.target.value)}
              placeholder="default"
              className="h-8 w-24 rounded-md border bg-background px-2 font-mono text-sm outline-none focus:ring-1 focus:ring-ring"
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
