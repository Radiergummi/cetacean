import { api } from "@/api/client";
import type { Service, Task } from "@/api/types";
import InfoCard from "@/components/InfoCard";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { SliderNumberField } from "@/components/ui/slider-number-field";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { Pencil } from "lucide-react";
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

export function ReplicaCard({ service, tasks }: { service: Service; tasks: Task[] }) {
  const [scaleOpen, setScaleOpen] = useState(false);
  const [scaleValue, setScaleValue] = useState<number | undefined>();
  const [scaleValidationError, setScaleValidationError] = useState<string | null>(null);
  const scale = useAsyncAction();

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
    }
    setScaleValidationError(null);
    setScaleOpen(open);
  }

  async function submitScale() {
    if (scaleValue === undefined || scaleValue < 0) {
      setScaleValidationError("Enter a valid replica count");
      return;
    }
    setScaleValidationError(null);
    await scale.execute(async () => {
      await api.scaleService(service.ID, scaleValue);
      setScaleOpen(false);
    }, "Failed to scale");
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
      <Popover
        open={scaleOpen}
        onOpenChange={handleOpenChange}
        modal
      >
        <PopoverTrigger
          render={
            <Button
              variant="ghost"
              size="icon-xs"
              title="Scale service"
            >
              <Pencil className="size-3.5" />
            </Button>
          }
        />
        <PopoverContent
          className="w-52"
          align="end"
        >
          <SliderNumberField
            label="Scale replicas"
            value={scaleValue}
            onChange={setScaleValue}
            min={0}
            step={1}
          />
          {(scaleValidationError || scale.error) && (
            <p className="mb-2 text-xs text-red-600 dark:text-red-400">
              {scaleValidationError || scale.error}
            </p>
          )}
          <div className="flex gap-2">
            <Button
              size="sm"
              className="flex-1"
              onClick={() => void submitScale()}
              disabled={scale.loading}
            >
              {scale.loading && <Spinner className="size-3" />}
              Scale
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="flex-1"
              onClick={() => handleOpenChange(false)}
              disabled={scale.loading}
            >
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
