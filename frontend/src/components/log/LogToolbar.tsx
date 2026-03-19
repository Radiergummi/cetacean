import type { TimeRange, Level } from "./log-utils";
import { presets, toLocalInput, formatShortDate } from "./log-utils";
import { X, Clock, ChevronDown } from "lucide-react";
import { useState, useEffect, useRef } from "react";

export function TimeRangeSelector({
  value,
  onChange,
}: {
  value: TimeRange;
  onChange: (tr: TimeRange) => void;
}) {
  const [open, setOpen] = useState(false);
  const [customSince, setCustomSince] = useState("");
  const [customUntil, setCustomUntil] = useState("");
  const ref = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  // Sync custom inputs when opening
  useEffect(() => {
    if (open) {
      setCustomSince(value.since ? toLocalInput(value.since) : "");
      setCustomUntil(value.until ? toLocalInput(value.until) : "");
    }
  }, [open, value.since, value.until]);

  const applyCustom = () => {
    const since = customSince ? new Date(customSince).toISOString() : undefined;
    const until = customUntil ? new Date(customUntil).toISOString() : undefined;

    let label = "Custom";
    if (since && until) {
      label = `${formatShortDate(since)} \u2013 ${formatShortDate(until)}`;
    } else if (since) {
      label = `Since ${formatShortDate(since)}`;
    } else if (until) {
      label = `Until ${formatShortDate(until)}`;
    }

    onChange({ since, until, label });
    setOpen(false);
  };

  return (
    <div
      className="relative"
      ref={ref}
    >
      <button
        onClick={() => setOpen(!open)}
        data-active={value.since || value.until || undefined}
        className="inline-flex h-8 items-center gap-1.5 rounded-md border bg-background px-2.5 text-xs hover:bg-muted data-active:border-primary/30 data-active:bg-primary/10 data-active:text-primary"
        title="Time range"
      >
        <Clock className="size-3.5" />
        <span className="max-w-32 truncate">{value.label}</span>
        <ChevronDown className="size-3 opacity-50" />
      </button>

      {open && (
        <div className="absolute top-full left-0 z-50 mt-1 w-72 rounded-lg border bg-popover shadow-lg">
          {/* Presets */}
          <div className="border-b p-2">
            <div className="mb-1.5 px-1 text-[10px] font-medium tracking-wider text-muted-foreground uppercase">
              Presets
            </div>
            <div className="flex flex-wrap gap-1">
              {presets.map((p) => (
                <button
                  key={p.label}
                  onClick={() => {
                    onChange(p.getValue());
                    setOpen(false);
                  }}
                  aria-pressed={value.label === p.label}
                  className="rounded-md bg-muted px-2 py-1 text-xs text-foreground hover:bg-muted/80 aria-pressed:bg-primary aria-pressed:text-primary-foreground"
                >
                  {p.label}
                </button>
              ))}
            </div>
          </div>

          {/* Custom range */}
          <div className="p-2">
            <div className="mb-1.5 px-1 text-[10px] font-medium tracking-wider text-muted-foreground uppercase">
              Custom Range
            </div>
            <div className="space-y-2">
              <label className="flex items-center gap-2 text-xs">
                <span className="w-10 text-muted-foreground">From</span>
                <input
                  type="datetime-local"
                  value={customSince}
                  onChange={(e) => setCustomSince(e.target.value)}
                  className="h-7 flex-1 rounded-md border bg-background px-2 text-xs"
                />
                {customSince && (
                  <button
                    onClick={() => setCustomSince("")}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="size-3" />
                  </button>
                )}
              </label>
              <label className="flex items-center gap-2 text-xs">
                <span className="w-10 text-muted-foreground">To</span>
                <input
                  type="datetime-local"
                  value={customUntil}
                  onChange={(e) => setCustomUntil(e.target.value)}
                  className="h-7 flex-1 rounded-md border bg-background px-2 text-xs"
                />
                {customUntil && (
                  <button
                    onClick={() => setCustomUntil("")}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="size-3" />
                  </button>
                )}
              </label>
              <button
                onClick={applyCustom}
                disabled={!customSince && !customUntil}
                className="h-7 w-full rounded-md bg-primary text-xs font-medium text-primary-foreground disabled:opacity-40"
              >
                Apply
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export function LevelFilter({
  value,
  onChange,
}: {
  value: Level | "all";
  onChange: (v: Level | "all") => void;
}) {
  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value as Level | "all")}
      title="Filter by level"
      className="h-8 rounded-md border bg-background px-2 text-xs"
    >
      <option value="all">All levels</option>
      <option value="error">Error</option>
      <option value="warn">Warn</option>
      <option value="info">Info</option>
      <option value="debug">Debug</option>
    </select>
  );
}

const STREAM_OPTIONS = ["all", "stdout", "stderr"] as const;

export function StreamFilterToggle({
  value,
  onChange,
}: {
  value: "all" | "stdout" | "stderr";
  onChange: (v: "all" | "stdout" | "stderr") => void;
}) {
  return (
    <div className="flex h-8 items-center overflow-hidden rounded-md border bg-background">
      {STREAM_OPTIONS.map((opt) => (
        <button
          key={opt}
          onClick={() => onChange(opt)}
          aria-pressed={value === opt}
          className="h-full px-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground aria-pressed:bg-primary aria-pressed:text-primary-foreground"
          title={opt === "all" ? "All streams" : opt}
        >
          {opt === "all" ? "All" : opt}
        </button>
      ))}
    </div>
  );
}

export { IconButton as ToolbarButton } from "../IconButton";
