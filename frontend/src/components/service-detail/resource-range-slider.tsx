import { Label } from "@/components/ui/label";
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
}

function toPosition(value: number | undefined, max: number, step: number, side: "reservation" | "limit"): number {
  if (value === undefined) return side === "reservation" ? 0 : max + step;
  return value;
}

function fromPosition(position: number, max: number, step: number, side: "reservation" | "limit"): number | undefined {
  if (side === "reservation" && position === 0) return undefined;
  if (side === "limit" && position >= max + step) return undefined;
  return position;
}

export function ResourceRangeSlider({ label, reservation, limit, onChange, max, step }: ResourceRangeSliderProps) {
  const sliderMax = max + step; // extra position for ∞
  const reservationPosition = toPosition(reservation, max, step, "reservation");
  const limitPosition = toPosition(limit, max, step, "limit");

  const ticks = useMemo(() => computeTicks(max, step), [max, step]);

  // Dead zone width as percentage of total slider width
  const deadZonePercent = (step / sliderMax) * 100;

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
    // Push limit if reservation exceeds it
    const newLimit = limit !== undefined && clamped > limit ? clamped : limit;
    onChange({ reservation: clamped, limit: newLimit });
  }

  function handleLimitInput(next: number | null) {
    if (next === null || next === undefined) {
      onChange({ reservation, limit: undefined });
      return;
    }
    const clamped = Math.max(step, Math.min(max, next));
    // Push reservation down if limit is below it
    const newReservation = reservation !== undefined && clamped < reservation ? clamped : reservation;
    onChange({ reservation: newReservation, limit: clamped });
  }

  const isReservationActive = reservation !== undefined;
  const isLimitActive = limit !== undefined;

  return (
    <div className="flex flex-col gap-1 w-full">
      <Label className="text-xs text-muted-foreground">{label}</Label>

      {/* Slider with dead zones */}
      <div className="relative">
        <SliderPrimitive.Root
          value={[reservationPosition, limitPosition]}
          onValueChange={handleSliderChange}
          min={0}
          max={sliderMax}
          step={step}
          className="data-horizontal:w-full"
        >
          <SliderPrimitive.Control className="relative flex w-full touch-none items-center select-none h-6">
            <SliderPrimitive.Track className="relative grow overflow-hidden rounded-full bg-transparent select-none data-horizontal:h-1.5 data-horizontal:w-full">
              {/* Dead zone left */}
              <div
                className="absolute top-1/2 left-0 h-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/20"
                style={{ width: `${deadZonePercent}%` }}
              />
              {/* Main track */}
              <div
                className="absolute top-0 h-full rounded-full bg-muted"
                style={{ left: `${deadZonePercent}%`, right: `${deadZonePercent}%` }}
              />
              {/* Dead zone right */}
              <div
                className="absolute top-1/2 right-0 h-0.5 -translate-y-1/2 rounded-full bg-muted-foreground/20"
                style={{ width: `${deadZonePercent}%` }}
              />
              {/* Filled range indicator */}
              <SliderPrimitive.Indicator
                className={`data-horizontal:h-full bg-primary ${!isReservationActive && !isLimitActive ? "opacity-20" : ""}`}
              />
            </SliderPrimitive.Track>
            {/* Reservation thumb */}
            <SliderPrimitive.Thumb
              className={`relative block size-3.5 shrink-0 rounded-full border-2 transition-[color,box-shadow] select-none after:absolute after:-inset-2 hover:ring-3 focus-visible:ring-3 focus-visible:outline-hidden active:ring-3 ${
                isReservationActive
                  ? "border-primary bg-white ring-primary/50"
                  : "border-muted-foreground/40 bg-muted ring-muted-foreground/20"
              }`}
            />
            {/* Limit thumb */}
            <SliderPrimitive.Thumb
              className={`relative block size-3.5 shrink-0 rounded-full border-2 transition-[color,box-shadow] select-none after:absolute after:-inset-2 hover:ring-3 focus-visible:ring-3 focus-visible:outline-hidden active:ring-3 ${
                isLimitActive
                  ? "border-primary bg-white ring-primary/50"
                  : "border-muted-foreground/40 bg-muted ring-muted-foreground/20"
              }`}
            />
          </SliderPrimitive.Control>
        </SliderPrimitive.Root>

        {/* Tick marks */}
        <div className="relative h-3" style={{ marginLeft: `${deadZonePercent}%`, marginRight: `${deadZonePercent}%` }}>
          {ticks.map((tick) => (
            <div
              key={tick.value}
              className="absolute top-0 flex flex-col items-center -translate-x-1/2"
              style={{ left: `${((tick.value - step) / (max - step)) * 100}%` }}
            >
              <div className={`w-px bg-muted-foreground/40 ${tick.tall ? "h-2.5" : "h-1.5"}`} />
            </div>
          ))}
        </div>
      </div>

      {/* Number inputs: reservation left, limit right */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <span className="text-[10px] text-muted-foreground uppercase tracking-wide">Reserved</span>
          {isReservationActive ? (
            <NumberField.Root
              value={reservation}
              onValueChange={handleReservationInput}
              min={step}
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
          ) : (
            <span className="inline-flex size-6 items-center justify-center rounded-md border text-xs text-muted-foreground">&mdash;</span>
          )}
        </div>
        <div className="flex items-center gap-1.5">
          {isLimitActive ? (
            <NumberField.Root
              value={limit}
              onValueChange={handleLimitInput}
              min={step}
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
          ) : (
            <span className="inline-flex size-6 items-center justify-center rounded-md border text-xs text-muted-foreground">&mdash;</span>
          )}
          <span className="text-[10px] text-muted-foreground uppercase tracking-wide">Limit</span>
        </div>
      </div>
    </div>
  );
}

/** Generate tick positions. Boundary ticks (step and max) are tall; intermediate ticks are short. */
export function computeTicks(max: number, step: number): Array<{ value: number; tall: boolean }> {
  const ticks: Array<{ value: number; tall: boolean }> = [];

  // Determine intermediate tick interval
  // For CPU (step ≤ 1): every whole core
  // For memory (step > 1): power-of-two boundaries
  let interval: number;
  if (step <= 1) {
    interval = 1;
  } else {
    // Find a nice power-of-two interval that gives ~4-8 ticks
    const range = max - step;
    const targetTicks = 6;
    const raw = range / targetTicks;
    interval = Math.pow(2, Math.round(Math.log2(raw)));
    if (interval < step) interval = step;
  }

  // Boundary tick at min (step)
  ticks.push({ value: step, tall: true });

  // Intermediate ticks
  const firstIntermediate = Math.ceil((step + 0.001) / interval) * interval;
  for (let value = firstIntermediate; value < max; value += interval) {
    if (Math.abs(value - step) > step * 0.01 && Math.abs(value - max) > step * 0.01) {
      ticks.push({ value, tall: false });
    }
  }

  // Boundary tick at max
  if (max > step) {
    ticks.push({ value: max, tall: true });
  }

  return ticks;
}
