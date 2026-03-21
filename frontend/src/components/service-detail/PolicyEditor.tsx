import { api } from "@/api/client";
import type { UpdateConfig } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { RadioCard } from "@/components/ui/radio-card";
import { SliderNumberField } from "@/components/ui/slider-number-field";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { formatDuration, formatPercentage, nanosToSeconds } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { useState } from "react";

interface PolicyEditorProps {
  type: "update" | "rollback";
  serviceId: string;
  policy: UpdateConfig | null;
  onSaved: () => void;
}

interface FormState {
  parallelism: number;
  delaySeconds: number;
  monitorSeconds: number;
  maxFailureRatio: number;
  failureAction: string;
  order: string;
}

function policyToForm(policy: UpdateConfig | null): FormState {
  return {
    parallelism: policy?.Parallelism ?? 1,
    delaySeconds: nanosToSeconds(policy?.Delay) ?? 0,
    monitorSeconds: nanosToSeconds(policy?.Monitor) ?? 0,
    maxFailureRatio: policy?.MaxFailureRatio ?? 0,
    failureAction: policy?.FailureAction ?? "pause",
    order: policy?.Order ?? "stop-first",
  };
}

const titles: Record<string, string> = {
  update: "Update Policy",
  rollback: "Rollback Policy",
};

export function PolicyEditor({ type, serviceId, policy, onSaved }: PolicyEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.configuration;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(policyToForm(null));
  useEscapeCancel(editing, () => cancelEdit());

  const patchFunction =
    type === "update" ? api.patchServiceUpdatePolicy : api.patchServiceRollbackPolicy;

  const failureActions =
    type === "update"
      ? [
          {
            value: "pause",
            title: "Pause",
            description: "Pause the deployment and wait for manual intervention.",
          },
          {
            value: "continue",
            title: "Continue",
            description: "Continue the deployment despite failures.",
          },
          {
            value: "rollback",
            title: "Rollback",
            description: "Automatically roll back to the previous version.",
          },
        ]
      : [
          {
            value: "pause",
            title: "Pause",
            description: "Pause the rollback and wait for manual intervention.",
          },
          {
            value: "continue",
            title: "Continue",
            description: "Continue the rollback despite failures.",
          },
        ];

  function openEdit() {
    setForm(policyToForm(policy));
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
      const patch: Record<string, unknown> = {
        Parallelism: form.parallelism,
        Delay: Math.round(form.delaySeconds * 1e9),
        Monitor: Math.round(form.monitorSeconds * 1e9),
        MaxFailureRatio: form.maxFailureRatio,
        FailureAction: form.failureAction,
        Order: form.order,
      };

      await patchFunction(serviceId, patch as UpdateConfig);

      setEditing(false);
      onSaved();
    } catch (error) {
      setSaveError(getErrorMessage(error, `Failed to update ${type} policy`));
    } finally {
      setSaving(false);
    }
  }

  if (!editing) {
    const rows = [
      ["Parallelism", String(policy?.Parallelism ?? 1)],
      policy?.Delay != null && ["Delay", formatDuration(policy.Delay)],
      policy?.FailureAction != null && ["Failure Action", policy.FailureAction],
      policy?.Monitor != null && ["Monitor", formatDuration(policy.Monitor)],
      policy?.MaxFailureRatio != null && [
        "Max Failure Ratio",
        formatPercentage(policy.MaxFailureRatio * 100, 0),
      ],
      policy?.Order != null && ["Order", policy.Order],
    ].filter(Boolean) as [string, string][];

    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            {titles[type]}
          </h3>

          {canEdit && (
            <Button
              variant="outline"
              size="xs"
              onClick={openEdit}
            >
              <Pencil className="size-3" />
              Edit
            </Button>
          )}
        </div>

        {rows.length > 0 ? (
          <div className="space-y-1">
            {rows.map(([label, value]) => (
              <div
                key={label}
                className="flex justify-between text-sm"
              >
                <span className="text-muted-foreground">{label}</span>
                <span className="font-mono">{value}</span>
              </div>
            ))}
          </div>
        ) : (
          <div className="flex flex-col items-center gap-1 rounded-lg border border-dashed py-6 text-center text-muted-foreground">
            <p className="text-sm">No {type} policy configured</p>
            {canEdit && (
              <p className="text-xs">
                {type === "update"
                  ? "Click Edit to control how new versions are rolled out across tasks."
                  : "Click Edit to control how failed updates are automatically reverted."}
              </p>
            )}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
        {titles[type]}
      </h3>

      <div className="grid grid-cols-2 gap-3">
        <SliderNumberField
          label="Parallelism"
          value={form.parallelism}
          onChange={(value) => setForm({ ...form, parallelism: value ?? 0 })}
          min={0}
          step={1}
        />

        <SliderNumberField
          label="Delay (s)"
          value={form.delaySeconds || undefined}
          onChange={(value) => setForm({ ...form, delaySeconds: value ?? 0 })}
          min={0}
          step={0.1}
        />

        <SliderNumberField
          label="Monitor (s)"
          value={form.monitorSeconds || undefined}
          onChange={(value) => setForm({ ...form, monitorSeconds: value ?? 0 })}
          min={0}
          step={0.1}
        />

        <SliderNumberField
          label="Max failure ratio"
          value={form.maxFailureRatio || undefined}
          onChange={(value) => setForm({ ...form, maxFailureRatio: Math.min(value ?? 0, 1) })}
          min={0}
          step={0.05}
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <span className="text-xs font-medium text-muted-foreground">Failure action</span>

        <div
          className={
            type === "update"
              ? "grid grid-cols-1 gap-1.5 sm:grid-cols-3"
              : "grid grid-cols-1 gap-1.5 sm:grid-cols-2"
          }
        >
          {failureActions.map(({ value, title, description }) => (
            <RadioCard
              key={value}
              selected={form.failureAction === value}
              onClick={() => setForm({ ...form, failureAction: value })}
              title={title}
              description={description}
            />
          ))}
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <span className="text-xs font-medium text-muted-foreground">Order</span>

        <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
          <RadioCard
            selected={form.order === "stop-first"}
            onClick={() => setForm({ ...form, order: "stop-first" })}
            title="Stop old first"
            description="Stop old tasks before starting new ones. Minimizes resource usage."
          />
          <RadioCard
            selected={form.order === "start-first"}
            onClick={() => setForm({ ...form, order: "start-first" })}
            title="Start new first"
            description="Start new tasks before stopping old ones. Minimizes downtime."
          />
        </div>
      </div>

      {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}

      <footer className="flex items-center justify-end gap-2">
        <Button
          size="sm"
          onClick={save}
          disabled={saving}
        >
          {saving && <Spinner className="size-3" />}
          Save
        </Button>

        <Button
          variant="outline"
          size="sm"
          onClick={cancelEdit}
          disabled={saving}
        >
          Cancel
        </Button>
      </footer>
    </div>
  );
}
