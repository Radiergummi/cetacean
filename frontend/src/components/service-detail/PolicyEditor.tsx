import { DockerDocsLink } from "./DockerDocsLink";
import { EditablePanel } from "./EditablePanel";
import { api } from "@/api/client";
import type { UpdateConfig } from "@/api/types";
import { NumberField } from "@/components/ui/number-field";
import { RadioCard, RadioCardGroup } from "@/components/ui/radio-card";
import { formatDuration, formatPercentage, nanosToSeconds } from "@/lib/format";
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

const emptyDescriptions: Record<string, string> = {
  update: "Click Edit to control how new versions are rolled out across tasks.",
  rollback: "Click Edit to control how failed updates are automatically reverted.",
};

export function PolicyEditor({
  type,
  serviceId,
  policy,
  onSaved,
  canEdit = false,
}: PolicyEditorProps & { canEdit?: boolean }) {
  const [form, setForm] = useState<FormState>(policyToForm(null));

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
            description: "Continue the deployment despite failures.",
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

  function resetForm() {
    setForm(policyToForm(policy));
  }

  async function save() {
    const patch: Record<string, unknown> = {
      Parallelism: form.parallelism,
      Delay: Math.round(form.delaySeconds * 1e9),
      Monitor: Math.round(form.monitorSeconds * 1e9),
      MaxFailureRatio: form.maxFailureRatio,
      FailureAction: form.failureAction,
      Order: form.order,
    };

    await patchFunction(serviceId, patch as UpdateConfig);
    onSaved();
  }

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
    <EditablePanel
      title={titles[type]}
      bordered={false}
      canEdit={canEdit}
      empty={!policy}
      emptyDescription={emptyDescriptions[type]}
      onOpen={resetForm}
      onSave={save}
      display={
        rows.length > 0 ? (
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
        ) : null
      }
      edit={
        <>
          <div className="grid grid-cols-2 gap-3">
            <NumberField
              label={
                <span className="flex items-center gap-1">
                  Parallelism{" "}
                  <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#update-delay" />
                </span>
              }
              value={form.parallelism}
              onChange={(value) => setForm({ ...form, parallelism: value ?? 0 })}
              min={0}
              step={1}
            />

            <NumberField
              label={
                <span className="flex items-center gap-1">
                  Delay (s){" "}
                  <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#update-delay" />
                </span>
              }
              value={form.delaySeconds || undefined}
              onChange={(value) => setForm({ ...form, delaySeconds: value ?? 0 })}
              min={0}
              step={0.1}
            />

            <NumberField
              label={
                <span className="flex items-center gap-1">
                  Monitor (s){" "}
                  <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#update-delay" />
                </span>
              }
              value={form.monitorSeconds || undefined}
              onChange={(value) => setForm({ ...form, monitorSeconds: value ?? 0 })}
              min={0}
              step={0.1}
            />

            <NumberField
              label={
                <span className="flex items-center gap-1">
                  Max failure ratio{" "}
                  <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#update-delay" />
                </span>
              }
              value={form.maxFailureRatio || undefined}
              onChange={(value) => setForm({ ...form, maxFailureRatio: Math.min(value ?? 0, 1) })}
              min={0}
              step={0.05}
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <span className="flex items-center gap-1 text-xs font-medium text-muted-foreground">
              Failure action{" "}
              <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#update-delay" />
            </span>

            <RadioCardGroup
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
            </RadioCardGroup>
          </div>

          <div className="flex flex-col gap-1.5">
            <span className="flex items-center gap-1 text-xs font-medium text-muted-foreground">
              Order{" "}
              <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#update-delay" />
            </span>

            <RadioCardGroup className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
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
            </RadioCardGroup>
          </div>
        </>
      }
    />
  );
}
