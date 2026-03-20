import { api } from "@/api/client";
import type { UpdateConfig } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import { formatDuration } from "@/lib/format";
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

function nanosToSeconds(nanos: number | undefined): number {
  return nanos != null ? nanos / 1e9 : 0;
}

function policyToForm(policy: UpdateConfig | null): FormState {
  return {
    parallelism: policy?.Parallelism ?? 1,
    delaySeconds: nanosToSeconds(policy?.Delay),
    monitorSeconds: nanosToSeconds(policy?.Monitor),
    maxFailureRatio: policy?.MaxFailureRatio ?? 0,
    failureAction: policy?.FailureAction ?? "pause",
    order: policy?.Order ?? "stop-first",
  };
}

function formatRatio(ratio: number | undefined): string {
  if (ratio == null || ratio === 0) {
    return "0%";
  }

  return `${(ratio * 100).toFixed(0)}%`;
}

const titles: Record<string, string> = {
  update: "Update Policy",
  rollback: "Rollback Policy",
};

export function PolicyEditor({ type, serviceId, policy, onSaved }: PolicyEditorProps) {
  const { level } = useOperationsLevel();
  const canEdit = level >= 1;

  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [form, setForm] = useState<FormState>(policyToForm(null));

  const patchFunction =
    type === "update" ? api.patchServiceUpdatePolicy : api.patchServiceRollbackPolicy;

  const failureActions =
    type === "update" ? ["pause", "continue", "rollback"] : ["pause", "continue"];

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
      await patchFunction(serviceId, {
        Parallelism: form.parallelism,
        Delay: Math.round(form.delaySeconds * 1e9),
        Monitor: Math.round(form.monitorSeconds * 1e9),
        MaxFailureRatio: form.maxFailureRatio,
        FailureAction: form.failureAction,
        Order: form.order,
      });

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
      policy?.MaxFailureRatio != null && ["Max Failure Ratio", formatRatio(policy.MaxFailureRatio)],
      policy?.Order != null && ["Order", policy.Order],
    ].filter(Boolean) as [string, string][];

    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
            {titles[type]}
          </h3>

          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
            disabled={!canEdit}
            title={canEdit ? undefined : "Editing disabled by server configuration"}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        </div>

        {rows.length > 0 ? (
          <div className="space-y-1">
            {rows.map(([label, value]) => (
              <div key={label} className="flex justify-between text-sm">
                <span className="text-muted-foreground">{label}</span>
                <span className="font-mono">{value}</span>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No {type} policy configured.</p>
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
        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Parallelism</label>

          <Input
            type="number"
            min={0}
            value={form.parallelism}
            onChange={(event) => setForm({ ...form, parallelism: Number(event.target.value) || 0 })}
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Delay (seconds)</label>

          <Input
            type="number"
            min={0}
            step={0.1}
            value={form.delaySeconds || ""}
            onChange={(event) => setForm({ ...form, delaySeconds: Number(event.target.value) || 0 })}
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Monitor (seconds)</label>

          <Input
            type="number"
            min={0}
            step={0.1}
            value={form.monitorSeconds || ""}
            onChange={(event) =>
              setForm({ ...form, monitorSeconds: Number(event.target.value) || 0 })
            }
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Max failure ratio</label>

          <Input
            type="number"
            min={0}
            max={1}
            step={0.1}
            value={form.maxFailureRatio}
            onChange={(event) =>
              setForm({ ...form, maxFailureRatio: Number(event.target.value) || 0 })
            }
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Failure action</label>

          <select
            value={form.failureAction}
            onChange={(event) => setForm({ ...form, failureAction: event.target.value })}
            className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm"
          >
            {failureActions.map((action) => (
              <option key={action} value={action}>
                {action}
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">Order</label>

          <select
            value={form.order}
            onChange={(event) => setForm({ ...form, order: event.target.value })}
            className="flex h-8 w-full rounded-md border border-input bg-transparent px-3 text-sm"
          >
            <option value="stop-first">stop-first</option>
            <option value="start-first">start-first</option>
          </select>
        </div>
      </div>

      {saveError && <p className="text-xs text-red-600">{saveError}</p>}

      <div className="flex gap-2">
        <Button size="sm" onClick={save} disabled={saving}>
          {saving && <Spinner className="size-3" />}
          Save
        </Button>

        <Button variant="outline" size="sm" onClick={cancelEdit} disabled={saving}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
