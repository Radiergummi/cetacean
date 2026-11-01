import SegmentedControl from "../SegmentedControl";
import { rangeSegments } from "./MetricsPanelContext";
import { useTimeRange } from "./useTimeRange";
import { Calendar, X } from "lucide-react";
import { useState } from "react";

function formatRange(from: number, to: number): string {
  const fromDate = new Date(from * 1000);
  const toDate = new Date(to * 1000);
  const sameDay = fromDate.toDateString() === toDate.toDateString();
  const dateFmt: Intl.DateTimeFormatOptions = { month: "short", day: "numeric" };
  const timeFmt: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit" };

  if (sameDay) {
    return `${fromDate.toLocaleDateString(undefined, dateFmt)} ${fromDate.toLocaleTimeString(
      undefined,
      timeFmt,
    )} – ${toDate.toLocaleTimeString(undefined, timeFmt)}`;
  }

  return `${fromDate.toLocaleDateString(undefined, dateFmt)} ${fromDate.toLocaleTimeString(
    undefined,
    timeFmt,
  )} – ${toDate.toLocaleDateString(undefined, dateFmt)} ${toDate.toLocaleTimeString(undefined, timeFmt)}`;
}

const quickPresets = [
  { label: "Last 2h", seconds: 7200 },
  { label: "Last 12h", seconds: 43200 },
  { label: "Last 48h", seconds: 172800 },
  { label: "Last 3d", seconds: 259200 },
];

/**
 * The time range presets and a custom from/to picker, reading and writing the
 * range in the URL that every panel on the page shares.
 */
export default function TimeRangeControl({ scope }: { scope?: string | undefined }) {
  const { range, customFrom, customTo, setRange, setCustomRange, clearCustomRange } =
    useTimeRange(scope);
  const isCustomRange = customFrom != null && customTo != null;
  const [startInput, setStartInput] = useState("");
  const [endInput, setEndInput] = useState("");

  const handleCustomApply = (close: () => void) => {
    const start = new Date(startInput).getTime() / 1000;
    const end = new Date(endInput).getTime() / 1000;

    if (!isNaN(start) && !isNaN(end) && start < end) {
      setCustomRange(start, end);
      close();
    }
  };

  const handlePreset = (seconds: number, close: () => void) => {
    // Reading the clock is the point, and this runs on click rather than
    // during render — the rule flags it because the closure is built here.
    // oxlint-disable-next-line react/purity
    const now = Math.floor(Date.now() / 1000);

    setCustomRange(now - seconds, now);
    close();
  };

  const customRangeLabel = isCustomRange ? formatRange(customFrom!, customTo!) : undefined;

  return (
    <SegmentedControl
      segments={rangeSegments}
      value={isCustomRange ? ("" as string) : range}
      onChange={(value) => setRange(value)}
      overflowIcon={<Calendar className="size-3" />}
      overflowName="Custom range"
      overflowActive={isCustomRange}
      overflowLabel={
        isCustomRange ? (
          <span className="flex items-center gap-1 py-0.5 text-xs">
            {customRangeLabel}
            <button
              type="button"
              className="cursor-pointer opacity-60 hover:opacity-100"
              aria-label="Clear custom range"
              onClick={(event) => {
                event.stopPropagation();
                clearCustomRange();
              }}
            >
              <X className="size-3" />
            </button>
          </span>
        ) : undefined
      }
      overflowContent={(close) => (
        <div className="w-64 p-2">
          <div className="mb-3 grid grid-cols-2 gap-1.5">
            {quickPresets.map(({ label, seconds }) => (
              <button
                type="button"
                key={label}
                onClick={() => handlePreset(seconds, close)}
                className="rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              >
                {label}
              </button>
            ))}
          </div>
          <div className="flex flex-col gap-2">
            <label className="block">
              <span className="text-xs text-muted-foreground">From</span>
              <input
                type="datetime-local"
                value={startInput}
                onChange={(event) => setStartInput(event.target.value)}
                className="mt-0.5 w-full rounded-md border bg-card px-2 py-1 text-xs"
              />
            </label>
            <label className="block">
              <span className="text-xs text-muted-foreground">To</span>
              <input
                type="datetime-local"
                value={endInput}
                onChange={(event) => setEndInput(event.target.value)}
                className="mt-0.5 w-full rounded-md border bg-card px-2 py-1 text-xs"
              />
            </label>
            <button
              type="button"
              onClick={() => handleCustomApply(close)}
              className="w-full rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90"
            >
              Apply
            </button>
          </div>
        </div>
      )}
    />
  );
}
