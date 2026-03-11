import { useState, useEffect, useRef } from "react";
import { X, Clock, ChevronDown } from "lucide-react";
import type { TimeRange, Level } from "./log-utils";
import { PRESETS, toLocalInput, formatShortDate } from "./log-utils";

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
  }, [open]);

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
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen(!open)}
        data-active={value.since || value.until || undefined}
        className="h-8 inline-flex items-center gap-1.5 px-2.5 text-xs border rounded-md bg-background hover:bg-muted data-active:bg-primary/10 data-active:border-primary/30 data-active:text-primary"
        title="Time range"
      >
        <Clock className="size-3.5" />
        <span className="max-w-32 truncate">{value.label}</span>
        <ChevronDown className="size-3 opacity-50" />
      </button>

      {open && (
        <div className="absolute top-full left-0 mt-1 z-50 w-72 rounded-lg border bg-popover shadow-lg">
          {/* Presets */}
          <div className="p-2 border-b">
            <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground mb-1.5 px-1">
              Presets
            </div>
            <div className="flex flex-wrap gap-1">
              {PRESETS.map((p) => (
                <button
                  key={p.label}
                  onClick={() => {
                    onChange(p.getValue());
                    setOpen(false);
                  }}
                  aria-selected={value.label === p.label || undefined}
                  className="px-2 py-1 text-xs rounded-md bg-muted hover:bg-muted/80 text-foreground aria-selected:bg-primary aria-selected:text-primary-foreground"
                >
                  {p.label}
                </button>
              ))}
            </div>
          </div>

          {/* Custom range */}
          <div className="p-2">
            <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground mb-1.5 px-1">
              Custom Range
            </div>
            <div className="space-y-2">
              <label className="flex items-center gap-2 text-xs">
                <span className="w-10 text-muted-foreground">From</span>
                <input
                  type="datetime-local"
                  value={customSince}
                  onChange={(e) => setCustomSince(e.target.value)}
                  className="flex-1 h-7 px-2 text-xs border rounded-md bg-background"
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
                  className="flex-1 h-7 px-2 text-xs border rounded-md bg-background"
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
                className="w-full h-7 text-xs font-medium rounded-md bg-primary text-primary-foreground disabled:opacity-40"
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
      className="h-8 px-2 text-xs border rounded-md bg-background"
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
    <div className="flex items-center h-8 rounded-md border bg-background overflow-hidden">
      {STREAM_OPTIONS.map((opt) => (
        <button
          key={opt}
          onClick={() => onChange(opt)}
          aria-pressed={value === opt}
          className="px-2 h-full text-xs text-muted-foreground hover:text-foreground hover:bg-muted aria-pressed:bg-primary aria-pressed:text-primary-foreground"
          title={opt === "all" ? "All streams" : opt}
        >
          {opt === "all" ? "All" : opt}
        </button>
      ))}
    </div>
  );
}

export function ToolbarButton({
  onClick,
  title,
  icon,
  active,
}: {
  onClick: () => void;
  title: string;
  icon: React.ReactNode;
  active?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-pressed={active || undefined}
      className="h-8 w-8 flex items-center justify-center rounded-md border border-border bg-background hover:bg-muted aria-pressed:bg-primary aria-pressed:text-primary-foreground aria-pressed:border-primary"
    >
      {icon}
    </button>
  );
}
