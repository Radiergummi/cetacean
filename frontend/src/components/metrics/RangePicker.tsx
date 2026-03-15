import { useState, useRef, useEffect } from "react";
import { Calendar, X } from "lucide-react";

interface Props {
  from: number | null;
  to: number | null;
  onApply: (from: number, to: number) => void;
  onClear: () => void;
}

const QUICK_PRESETS = [
  { label: "Last 2h", seconds: 7200 },
  { label: "Last 12h", seconds: 43200 },
  { label: "Last 48h", seconds: 172800 },
  { label: "Last 3d", seconds: 259200 },
];

function formatRange(from: number, to: number): string {
  const f = new Date(from * 1000);
  const t = new Date(to * 1000);
  const sameDay = f.toDateString() === t.toDateString();
  const dateFmt: Intl.DateTimeFormatOptions = { month: "short", day: "numeric" };
  const timeFmt: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit" };
  if (sameDay) {
    return `${f.toLocaleDateString(undefined, dateFmt)} ${f.toLocaleTimeString(undefined, timeFmt)} – ${t.toLocaleTimeString(undefined, timeFmt)}`;
  }
  return `${f.toLocaleDateString(undefined, dateFmt)} ${f.toLocaleTimeString(undefined, timeFmt)} – ${t.toLocaleDateString(undefined, dateFmt)} ${t.toLocaleTimeString(undefined, timeFmt)}`;
}

export default function RangePicker({ from, to, onApply, onClear }: Props) {
  const [open, setOpen] = useState(false);
  const [startInput, setStartInput] = useState("");
  const [endInput, setEndInput] = useState("");
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const handleApply = () => {
    const s = new Date(startInput).getTime() / 1000;
    const e = new Date(endInput).getTime() / 1000;
    if (!isNaN(s) && !isNaN(e) && s < e) {
      onApply(s, e);
      setOpen(false);
    }
  };

  const handlePreset = (seconds: number) => {
    const now = Math.floor(Date.now() / 1000);
    onApply(now - seconds, now);
    setOpen(false);
  };

  const isActive = from != null && to != null;

  return (
    <div ref={ref} className="relative">
      {isActive ? (
        <button
          onClick={onClear}
          className="inline-flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md border bg-primary/10 border-primary/30 text-foreground hover:bg-primary/20"
        >
          <Calendar className="size-3" />
          <span>{formatRange(from!, to!)}</span>
          <X className="size-3 opacity-60 hover:opacity-100" />
        </button>
      ) : (
        <button
          onClick={() => setOpen(!open)}
          className="inline-flex items-center justify-center size-7 rounded-md border border-border bg-card hover:bg-muted"
          title="Custom range"
        >
          <Calendar className="size-3.5" />
        </button>
      )}

      {open && (
        <div className="absolute right-0 top-full mt-1 z-30 w-72 rounded-lg border bg-popover shadow-lg p-3 text-sm">
          <div className="grid grid-cols-2 gap-1.5 mb-3">
            {QUICK_PRESETS.map((p) => (
              <button
                key={p.label}
                onClick={() => handlePreset(p.seconds)}
                className="px-2 py-1.5 rounded-md text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                {p.label}
              </button>
            ))}
          </div>
          <div className="space-y-2">
            <label className="block">
              <span className="text-xs text-muted-foreground">From</span>
              <input
                type="datetime-local"
                value={startInput}
                onChange={(e) => setStartInput(e.target.value)}
                className="mt-0.5 w-full rounded-md border bg-card px-2 py-1 text-xs"
              />
            </label>
            <label className="block">
              <span className="text-xs text-muted-foreground">To</span>
              <input
                type="datetime-local"
                value={endInput}
                onChange={(e) => setEndInput(e.target.value)}
                className="mt-0.5 w-full rounded-md border bg-card px-2 py-1 text-xs"
              />
            </label>
            <button
              onClick={handleApply}
              className="w-full rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90"
            >
              Apply
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
