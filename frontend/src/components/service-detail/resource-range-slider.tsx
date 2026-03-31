import { NumberField } from "@base-ui/react/number-field";
import { Slider as SliderPrimitive } from "@base-ui/react/slider";
import { Minus, Plus } from "lucide-react";
import type React from "react";
import { useMemo } from "react";

interface ResourceRangeSliderProps {
  label: React.ReactNode;
  reservation: number | undefined;
  limit: number | undefined;
  onChange: (values: { reservation: number | undefined; limit: number | undefined }) => void;
  max: number;
  step: number;
  unit?: string;
  formatLabel: (value: number) => string;
}

const thumbClassName =
  "relative block size-3.5 shrink-0 rounded-full border-2 border-primary bg-white ring-primary/50 " +
  "transition-[color,box-shadow] select-none after:absolute after:-inset-2 hover:ring-3 focus-visible:ring-3 " +
  "focus-visible:outline-hidden active:ring-3";

const deadZonePercent = 5;

export function ResourceRangeSlider({
  label,
  reservation,
  limit,
  onChange,
  max,
  step,
  unit,
  formatLabel,
}: ResourceRangeSliderProps) {
  const sliderMax = max + step;
  const reservationPosition = reservation ?? 0;
  const limitPosition = limit ?? sliderMax;

  const ticks = useMemo(() => computeTicks(max, step, formatLabel), [max, step, formatLabel]);

  // Compute fill position, clamped to the main track (never into dead zones)
  const fillLeft = Math.max(deadZonePercent, (reservationPosition / sliderMax) * 100);
  const fillRight = Math.max(deadZonePercent, 100 - (limitPosition / sliderMax) * 100);

  const isReservationActive = reservation !== undefined;
  const isLimitActive = limit !== undefined;

  function handleSliderChange(positions: number[]) {
    const reservation =
      positions[0] <= step / 2 ? undefined : Math.round(positions[0] / step) * step;
    const newLimit =
      positions[1] >= max + step / 2 ? undefined : Math.round(positions[1] / step) * step;

    onChange({ reservation, limit: newLimit });
  }

  function handleReservationInput(next: number | null) {
    if (next === null || next === undefined) {
      onChange({ reservation: undefined, limit });

      return;
    }

    const clamped = Math.max(step, Math.min(max, next));
    const newLimit = limit !== undefined && clamped > limit ? clamped : limit;

    onChange({ reservation: clamped, limit: newLimit });
  }

  function handleLimitInput(next: number | null) {
    if (next === null || next === undefined) {
      onChange({ reservation, limit: undefined });

      return;
    }
    const clamped = Math.max(step, Math.min(max, next));
    const newReservation =
      reservation !== undefined && clamped < reservation ? clamped : reservation;

    onChange({ reservation: newReservation, limit: clamped });
  }

  return (
    <div className="flex w-full flex-col gap-1">
      <span className="text-xs text-muted-foreground">{label}</span>

      <div className="relative">
        <div className="pointer-events-none absolute inset-x-0 top-0 flex h-6 items-center">
          <div className="absolute inset-x-0 top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-muted" />
          <div
            className={`absolute top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-primary ${
              !isReservationActive && !isLimitActive ? "opacity-40" : ""
            }`}
            style={{ left: `${fillLeft}%`, right: `${fillRight}%` }}
          />
          <div
            className="absolute top-1/2 left-0 h-1.5 -translate-y-1/2 bg-background"
            style={{ width: `calc(${deadZonePercent}% - 2px)` }}
          />
          <div
            className="absolute top-1/2 right-0 h-1.5 -translate-y-1/2 bg-background"
            style={{ width: `calc(${deadZonePercent}% - 2px)` }}
          />
          <div
            className="absolute top-1/2 h-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/30"
            style={{ left: 0, width: `${deadZonePercent}%` }}
          />
          <div
            className="absolute top-1/2 h-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/30"
            style={{ right: 0, width: `${deadZonePercent}%` }}
          />
        </div>

        <SliderPrimitive.Root
          value={[reservationPosition, limitPosition]}
          onValueChange={handleSliderChange}
          min={0}
          max={sliderMax}
          step={sliderMax / 1000}
          className="data-horizontal:w-full"
        >
          <SliderPrimitive.Control className="relative flex h-6 w-full touch-none items-center select-none">
            <SliderPrimitive.Track className="relative grow opacity-0 select-none data-horizontal:w-full" />
            <SliderPrimitive.Thumb className={thumbClassName} />
            <SliderPrimitive.Thumb className={thumbClassName} />
          </SliderPrimitive.Control>
        </SliderPrimitive.Root>

        <div
          className="relative h-6"
          style={{ marginLeft: `${deadZonePercent}%`, marginRight: `${deadZonePercent}%` }}
        >
          {ticks.map((tick) => (
            <div
              key={tick.value}
              className="absolute top-0 flex -translate-x-1/2 flex-col items-center"
              style={{ left: `${((tick.value - step) / (max - step)) * 100}%` }}
            >
              <div className={`w-px bg-muted-foreground/40 ${tick.tall ? "h-2.5" : "h-1.5"}`} />
              {tick.label != null && (
                <span className="mt-0.5 whitespace-nowrap text-[9px] text-muted-foreground">{tick.label}</span>
              )}
            </div>
          ))}
        </div>
      </div>

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <span className="text-[10px] tracking-wide text-muted-foreground uppercase">
            Reserved
          </span>
          <CompactStepper
            value={reservation}
            onChange={handleReservationInput}
            min={step}
            max={max}
            step={step}
            unit={unit}
          />
        </div>
        <div className="flex items-center gap-1.5">
          <CompactStepper
            value={limit}
            onChange={handleLimitInput}
            min={step}
            max={max}
            step={step}
            unit={unit}
          />
          <span className="text-[10px] tracking-wide text-muted-foreground uppercase">Limit</span>
        </div>
      </div>
    </div>
  );
}

function CompactStepper({
  value,
  onChange,
  min,
  max,
  step,
  unit,
}: {
  value: number | undefined;
  onChange: (next: number | null) => void;
  min: number;
  max: number;
  step: number;
  unit?: string;
}) {
  if (value === undefined) {
    return (
      <span className="inline-flex size-6 items-center justify-center rounded-md border text-xs text-muted-foreground">
        &mdash;
      </span>
    );
  }
  return (
    <div className="flex items-center gap-1">
      <NumberField.Root
        value={value}
        onValueChange={onChange}
        min={min}
        max={max}
        step={step}
      >
        <NumberField.Group className="flex items-center rounded-md border focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
          <NumberField.Decrement className="flex size-6 items-center justify-center border-r text-muted-foreground hover:bg-accent disabled:opacity-50">
            <Minus className="size-2.5" />
          </NumberField.Decrement>
          <NumberField.Input className="w-12 bg-transparent px-1 py-0.5 text-center font-mono text-xs focus:outline-none" />
          <NumberField.Increment className="flex size-6 items-center justify-center border-l text-muted-foreground hover:bg-accent disabled:opacity-50">
            <Plus className="size-2.5" />
          </NumberField.Increment>
        </NumberField.Group>
      </NumberField.Root>

      {unit && <span className="text-[10px] text-muted-foreground">{unit}</span>}
    </div>
  );
}

interface Tick {
  value: number;
  tall: boolean;
  label?: string;
}

export function computeTicks(
  max: number,
  step: number,
  formatLabel: (value: number) => string,
): Tick[] {
  const ticks: Tick[] = [];

  let interval: number;

  // For CPU (step ≤ 1): ticks at every whole core. For memory: power-of-two intervals.
  if (step <= 1) {
    interval = 1;
  } else {
    const range = max - step;
    const raw = range / 6;
    interval = Math.pow(2, Math.round(Math.log2(raw)));

    if (interval < step) {
      interval = step;
    }
  }

  ticks.push({ value: step, tall: true, label: formatLabel(step) });

  for (let value = interval; value < max; value += interval) {
    if (Math.abs(value - step) > step * 0.01 && Math.abs(value - max) > step * 0.01) {
      ticks.push({ value, tall: false, label: formatLabel(value) });
    }
  }

  if (max > step) {
    ticks.push({ value: max, tall: true, label: formatLabel(max) });
  }

  return ticks;
}
