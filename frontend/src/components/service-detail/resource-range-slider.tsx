import { NumberField } from "@base-ui/react/number-field";
import { Slider as SliderPrimitive } from "@base-ui/react/slider";
import { Minus, Plus } from "lucide-react";
import { useMemo } from "react";

interface ResourceRangeSliderProps {
  label: string;
  reservation: number | undefined;
  limit: number | undefined;
  onChange: (values: { reservation: number | undefined; limit: number | undefined }) => void;
  max: number;
  step: number;
  formatLabel: (value: number) => string;
}

const THUMB_BASE =
  "relative block size-3.5 shrink-0 rounded-full border-2 transition-[color,box-shadow] select-none after:absolute after:-inset-2 hover:ring-3 focus-visible:ring-3 focus-visible:outline-hidden active:ring-3";
const THUMB_ACTIVE = "border-primary bg-white ring-primary/50";

const DEAD_ZONE_PERCENT = 5;

function toPosition(
  value: number | undefined,
  max: number,
  step: number,
  side: "reservation" | "limit",
): number {
  if (value === undefined) return side === "reservation" ? 0 : max + step;
  return value;
}

function fromPosition(
  position: number,
  max: number,
  step: number,
  side: "reservation" | "limit",
): number | undefined {
  if (side === "reservation" && position === 0) return undefined;
  if (side === "limit" && position >= max + step) return undefined;
  return position;
}

export function ResourceRangeSlider({
  label,
  reservation,
  limit,
  onChange,
  max,
  step,
  formatLabel,
}: ResourceRangeSliderProps) {
  const sliderMax = max + step;
  const reservationPosition = toPosition(reservation, max, step, "reservation");
  const limitPosition = toPosition(limit, max, step, "limit");

  const ticks = useMemo(() => computeTicks(max, step, formatLabel), [max, step, formatLabel]);

  // Compute fill position, clamped to the main track (never into dead zones)
  const fillLeft = Math.max(DEAD_ZONE_PERCENT, (reservationPosition / sliderMax) * 100);
  const fillRight = Math.max(DEAD_ZONE_PERCENT, 100 - (limitPosition / sliderMax) * 100);

  const isReservationActive = reservation !== undefined;
  const isLimitActive = limit !== undefined;

  function handleSliderChange(positions: number[]) {
    onChange({
      reservation: fromPosition(positions[0], max, step, "reservation"),
      limit: fromPosition(positions[1], max, step, "limit"),
    });
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
        {/* Visual track layers — rendered independently from Base UI's Track */}
        <div className="pointer-events-none absolute inset-x-0 top-0 flex h-6 items-center">
          {/* Full-width track background */}
          <div className="absolute inset-x-0 top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-muted" />
          {/* Filled range between thumbs */}
          <div
            className={`absolute top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-primary ${!isReservationActive && !isLimitActive ? "opacity-40" : ""}`}
            style={{ left: `${fillLeft}%`, right: `${fillRight}%` }}
          />
          {/* Dead zone masks — card background covers track ends to create visual gap */}
          <div
            className="absolute top-1/2 left-0 h-1.5 -translate-y-1/2 bg-background"
            style={{ width: `calc(${DEAD_ZONE_PERCENT}% - 2px)` }}
          />
          <div
            className="absolute top-1/2 right-0 h-1.5 -translate-y-1/2 bg-background"
            style={{ width: `calc(${DEAD_ZONE_PERCENT}% - 2px)` }}
          />
          {/* Thin dead zone lines */}
          <div
            className="absolute top-1/2 h-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/30"
            style={{ left: 0, width: `${DEAD_ZONE_PERCENT}%` }}
          />
          <div
            className="absolute top-1/2 h-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/30"
            style={{ right: 0, width: `${DEAD_ZONE_PERCENT}%` }}
          />
        </div>

        {/* Interactive slider — Track is invisible, only thumbs are visible */}
        <SliderPrimitive.Root
          value={[reservationPosition, limitPosition]}
          onValueChange={handleSliderChange}
          min={0}
          max={sliderMax}
          step={step}
          className="data-horizontal:w-full"
        >
          <SliderPrimitive.Control className="relative flex h-6 w-full touch-none items-center select-none">
            <SliderPrimitive.Track className="relative grow opacity-0 select-none data-horizontal:w-full" />
            <SliderPrimitive.Thumb className={`${THUMB_BASE} ${THUMB_ACTIVE}`} />
            <SliderPrimitive.Thumb className={`${THUMB_BASE} ${THUMB_ACTIVE}`} />
          </SliderPrimitive.Control>
        </SliderPrimitive.Root>

        {/* Tick marks with labels */}
        <div
          className="relative h-6"
          style={{ marginLeft: `${DEAD_ZONE_PERCENT}%`, marginRight: `${DEAD_ZONE_PERCENT}%` }}
        >
          {ticks.map((tick) => (
            <div
              key={tick.value}
              className="absolute top-0 flex -translate-x-1/2 flex-col items-center"
              style={{ left: `${((tick.value - step) / (max - step)) * 100}%` }}
            >
              <div className={`w-px bg-muted-foreground/40 ${tick.tall ? "h-2.5" : "h-1.5"}`} />
              {tick.label != null && (
                <span className="mt-0.5 text-[9px] text-muted-foreground">{tick.label}</span>
              )}
            </div>
          ))}
        </div>
      </div>

      {/* Number inputs: reservation left, limit right */}
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
          />
        </div>
        <div className="flex items-center gap-1.5">
          <CompactStepper
            value={limit}
            onChange={handleLimitInput}
            min={step}
            max={max}
            step={step}
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
}: {
  value: number | undefined;
  onChange: (next: number | null) => void;
  min: number;
  max: number;
  step: number;
}) {
  if (value === undefined) {
    return (
      <span className="inline-flex size-6 items-center justify-center rounded-md border text-xs text-muted-foreground">
        &mdash;
      </span>
    );
  }
  return (
    <NumberField.Root
      value={value}
      onValueChange={onChange}
      min={min}
      max={max}
      step={step}
    >
      <NumberField.Group className="flex items-center rounded-md border">
        <NumberField.Decrement className="flex size-6 items-center justify-center border-r text-muted-foreground hover:bg-accent disabled:opacity-50">
          <Minus className="size-2.5" />
        </NumberField.Decrement>
        <NumberField.Input className="w-12 bg-transparent px-1 py-0.5 text-center font-mono text-xs focus:outline-none" />
        <NumberField.Increment className="flex size-6 items-center justify-center border-l text-muted-foreground hover:bg-accent disabled:opacity-50">
          <Plus className="size-2.5" />
        </NumberField.Increment>
      </NumberField.Group>
    </NumberField.Root>
  );
}

interface Tick {
  value: number;
  tall: boolean;
  label?: string;
}

/** Generate tick positions. Boundary ticks (step and max) are tall with labels; intermediate ticks are short. */
export function computeTicks(
  max: number,
  step: number,
  formatLabel: (value: number) => string,
): Tick[] {
  const ticks: Tick[] = [];

  // For CPU (step ≤ 1): ticks at every whole core. For memory: power-of-two intervals.
  let interval: number;
  if (step <= 1) {
    interval = 1;
  } else {
    const range = max - step;
    const raw = range / 6;
    interval = Math.pow(2, Math.round(Math.log2(raw)));
    if (interval < step) interval = step;
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
