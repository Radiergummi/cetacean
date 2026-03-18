import { NumberField } from "@base-ui/react/number-field";
import { Minus, Pencil, Plus } from "lucide-react";
import { useState } from "react";
import { api } from "../../api/client";
import type { Service, Task } from "../../api/types";
import InfoCard from "../InfoCard";
import { Spinner } from "../Spinner";
import { Button } from "../ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "../ui/popover";

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

export function ReplicaCard({ service, tasks }: { service: Service; tasks: Task[] }) {
  const [scaleOpen, setScaleOpen] = useState(false);
  const [scaleValue, setScaleValue] = useState<number | null>(0);
  const [scaleLoading, setScaleLoading] = useState(false);
  const [scaleError, setScaleError] = useState<string | null>(null);

  const replicated = service.Spec.Mode.Replicated;
  if (!replicated) {
    return (
      <InfoCard
        label="Mode"
        value="global"
      />
    );
  }

  const desired = replicated.Replicas ?? 0;
  const running = tasks.filter((t) => t.Status.State === "running").length;
  const healthy = running >= desired;

  function handleOpenChange(open: boolean) {
    if (open) {
      setScaleValue(desired);
      setScaleError(null);
    } else {
      setScaleError(null);
    }
    setScaleOpen(open);
  }

  function cancelScale() {
    setScaleOpen(false);
    setScaleError(null);
  }

  async function submitScale() {
    if (scaleValue === null || scaleValue < 0) {
      setScaleError("Enter a valid replica count");
      return;
    }
    setScaleLoading(true);
    setScaleError(null);
    try {
      await api.scaleService(service.ID, scaleValue);
      setScaleOpen(false);
    } catch (err) {
      setScaleError(err instanceof Error ? err.message : "Failed to scale");
    } finally {
      setScaleLoading(false);
    }
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
    </>
  );

  const scaleControl = (
    <div className="flex items-center gap-2">
      {desired > 0 && (
        <ReplicaDoughnut
          running={running}
          desired={desired}
        />
      )}
      <Popover open={scaleOpen} onOpenChange={handleOpenChange} modal>
        <PopoverTrigger
          render={
            <Button variant="ghost" size="icon-xs" title="Scale service">
              <Pencil className="size-3.5" />
            </Button>
          }
        />
        <PopoverContent className="w-52" align="end">
          <p className="mb-2 text-xs font-medium text-muted-foreground">Scale replicas</p>
          <NumberField.Root
            value={scaleValue}
            onValueChange={(value) => setScaleValue(value)}
            min={0}
            step={1}
          >
            <NumberField.Group className="mb-2 flex items-center rounded-md border">
              <NumberField.Decrement className="flex size-8 items-center justify-center border-r text-muted-foreground hover:bg-accent disabled:opacity-50">
                <Minus className="size-3" />
              </NumberField.Decrement>
              <NumberField.Input className="w-full bg-transparent px-2 py-1 text-center font-mono text-sm focus:outline-none" />
              <NumberField.Increment className="flex size-8 items-center justify-center border-l text-muted-foreground hover:bg-accent disabled:opacity-50">
                <Plus className="size-3" />
              </NumberField.Increment>
            </NumberField.Group>
          </NumberField.Root>
          {scaleError && (
            <p className="mb-2 text-xs text-red-600 dark:text-red-400">{scaleError}</p>
          )}
          <div className="flex gap-2">
            <Button size="sm" className="flex-1" onClick={() => void submitScale()} disabled={scaleLoading}>
              {scaleLoading && <Spinner className="size-3" />}
              Scale
            </Button>
            <Button variant="outline" size="sm" className="flex-1" onClick={cancelScale} disabled={scaleLoading}>
              Cancel
            </Button>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );

  return (
    <InfoCard
      label="Replicas"
      value={value}
      right={scaleControl}
    />
  );
}
