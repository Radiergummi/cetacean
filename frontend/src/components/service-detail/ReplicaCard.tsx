import { api } from "@/api/client";
import type { Service, Task } from "@/api/types";
import InfoCard from "@/components/InfoCard";
import { DockerDocsLink } from "@/components/service-detail/DockerDocsLink";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { SliderNumberField } from "@/components/ui/slider-number-field";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { cn } from "@/lib/utils";
import { Copy, Globe, Pencil } from "lucide-react";
import { useState } from "react";

function ReplicaDoughnut({ running, desired }: { running: number; desired: number }) {
  const size = 50;
  const stroke = 5;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const ratio = desired > 0 ? Math.min(running / desired, 1) : 0;
  const offset = circumference * (1 - ratio);
  const healthy = running >= desired;

  if (healthy) {
    return (
      <svg
        width={size}
        height={size}
        viewBox={`0 0 ${size} ${size}`}
      >
        <circle
          cx={size / 2}
          cy={size / 2}
          r={size / 2}
          className="fill-green-500"
        />
        <path
          d="M15 25.5 L21.5 32 L35 19"
          fill="none"
          stroke="white"
          strokeWidth={3}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }

  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
    >
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        className="text-muted"
      />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        strokeLinecap="round"
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
        className="text-red-500"
      />
    </svg>
  );
}

type Mode = "replicated" | "global";

export function ReplicaCard({ service, tasks }: { service: Service; tasks: Task[] }) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canScale = !levelLoading && level >= opsLevel.operational;
  const canChangeMode = !levelLoading && level >= opsLevel.impactful;

  const currentMode: Mode = service.Spec.Mode.Replicated ? "replicated" : "global";
  const currentReplicas = service.Spec.Mode.Replicated?.Replicas ?? 0;

  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<Mode>(currentMode);
  const [replicas, setReplicas] = useState<number | undefined>(currentReplicas);
  const [validationError, setValidationError] = useState<string | null>(null);
  const action = useAsyncAction();

  function handleOpenChange(nextOpen: boolean) {
    if (nextOpen) {
      setMode(currentMode);
      setReplicas(currentReplicas || 1);
    }

    setValidationError(null);
    setOpen(nextOpen);
  }

  async function submit() {
    const modeChanged = mode !== currentMode;

    if (mode === "replicated" && (replicas === undefined || replicas < 0)) {
      setValidationError("Enter a valid replica count");

      return;
    }

    setValidationError(null);
    await action.execute(async () => {
      if (modeChanged) {
        await api.updateServiceMode(service.ID, mode, mode === "replicated" ? replicas : undefined);
      } else if (mode === "replicated") {
        await api.scaleService(service.ID, replicas!);
      }

      setOpen(false);
    }, "Failed to update service");
  }

  const isGlobal = currentMode === "global";
  const running = tasks.filter(({ Status: { State } }) => State === "running").length;
  const desired = isGlobal
    ? tasks.filter(({ DesiredState }) => DesiredState === "running").length
    : currentReplicas;
  const healthy = running >= desired;

  const editPopover = canScale ? (
    <Popover
      open={open}
      onOpenChange={handleOpenChange}
      modal
    >
      <Tooltip>
        <TooltipTrigger
          render={
            <PopoverTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-xs"
                >
                  <Pencil className="size-3.5" />
                </Button>
              }
            />
          }
        />
        <TooltipContent>Edit service mode</TooltipContent>
      </Tooltip>
      <PopoverContent
        className="w-72"
        align="end"
      >
        {canChangeMode && (
          <div className="mb-3 flex flex-col gap-2">
            <span className="flex items-center gap-1 text-xs font-medium text-muted-foreground">
              Mode{" "}
              <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#replicas" />
            </span>
            <button
              type="button"
              onClick={() => setMode("global")}
              disabled={action.loading}
              className={cn(
                "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors",
                mode === "global"
                  ? "border-primary bg-primary/5 ring-1 ring-primary"
                  : "border-border hover:border-muted-foreground/40",
                action.loading && "pointer-events-none opacity-50",
              )}
            >
              <Globe className="mt-0.5 size-4 shrink-0 text-muted-foreground" />

              <div className="flex-1">
                <div className="text-sm font-medium">Global</div>
                <div className="text-xs text-muted-foreground">
                  One task will run on every node in the swarm.
                </div>
              </div>

              <div
                className={cn(
                  "mt-0.5 size-4 shrink-0 rounded-full border-2 transition-colors",
                  mode === "global" ? "border-primary bg-primary" : "border-muted-foreground/40",
                )}
              >
                {mode === "global" && (
                  <div className="flex size-full items-center justify-center">
                    <div className="size-1.5 rounded-full bg-primary-foreground" />
                  </div>
                )}
              </div>
            </button>

            <button
              type="button"
              onClick={() => setMode("replicated")}
              disabled={action.loading}
              className={cn(
                "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors",
                mode === "replicated"
                  ? "border-primary bg-primary/5 ring-1 ring-primary"
                  : "border-border hover:border-muted-foreground/40",
                action.loading && "pointer-events-none opacity-50",
              )}
            >
              <Copy className="mt-0.5 size-4 shrink-0 text-muted-foreground" />

              <div className="flex-1">
                <div className="text-sm font-medium">Replicated</div>
                <div className="text-xs text-muted-foreground">
                  Run a specified number of tasks across the swarm.
                </div>
              </div>

              <div
                className={cn(
                  "mt-0.5 size-4 shrink-0 rounded-full border-2 transition-colors",
                  mode === "replicated"
                    ? "border-primary bg-primary"
                    : "border-muted-foreground/40",
                )}
              >
                {mode === "replicated" && (
                  <div className="flex size-full items-center justify-center">
                    <div className="size-1.5 rounded-full bg-primary-foreground" />
                  </div>
                )}
              </div>
            </button>
          </div>
        )}

        {mode === "replicated" && (
          <SliderNumberField
            label={
              <span className="flex items-center gap-1">
                Replicas{" "}
                <DockerDocsLink href="https://docs.docker.com/reference/cli/docker/service/create/#replicas" />
              </span>
            }
            value={replicas}
            onChange={setReplicas}
            min={0}
            step={1}
          />
        )}

        {(validationError || action.error) && (
          <p className="mb-2 text-xs text-red-600 dark:text-red-400">
            {validationError || action.error}
          </p>
        )}

        <div className="flex gap-2">
          <Button
            size="sm"
            className="flex-1"
            onClick={() => void submit()}
            disabled={action.loading}
          >
            {action.loading && <Spinner className="size-3" />}
            Apply
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="flex-1"
            onClick={() => handleOpenChange(false)}
            disabled={action.loading}
          >
            Cancel
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  ) : null;

  if (isGlobal) {
    return (
      <InfoCard
        label="Mode"
        value={
          <>
            <span className="capitalize">global</span>
            {editPopover}
          </>
        }
        right={
          desired > 0 ? (
            <ReplicaDoughnut
              running={running}
              desired={desired}
            />
          ) : undefined
        }
      />
    );
  }

  const value = (
    <>
      <span className="tabular-nums">
        <span className="text-2xl font-bold">{running}</span>
        <span className="text-lg font-normal text-muted-foreground">/{desired}</span>
      </span>

      {!healthy && (
        <div className="mt-1 text-xs text-red-600 dark:text-red-400">
          {desired - running} replica{desired - running !== 1 ? "s" : ""} not running
        </div>
      )}
      {editPopover}
    </>
  );

  return (
    <InfoCard
      label="Replicas"
      value={value}
      right={
        desired > 0 ? (
          <ReplicaDoughnut
            running={running}
            desired={desired}
          />
        ) : undefined
      }
    />
  );
}
