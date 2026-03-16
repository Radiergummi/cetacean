import {Calendar, X} from "lucide-react";
import {useEffect, useRef, useState} from "react";

interface Props {
    from: number | null;
    to: number | null;
    onApply: (from: number, to: number) => void;
    onClear: () => void;
}

const quickPresets = [
    {label: "Last 2h", seconds: 7_200},
    {label: "Last 12h", seconds: 43_200},
    {label: "Last 48h", seconds: 172_800},
    {label: "Last 3d", seconds: 259_200},
];

function formatDate(date: Date): string {
    return date.toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
    });
}

function formatTime(date: Date): string {
    return date.toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
    });
}

function formatRange(from: number, to: number): string {
    const startTime = new Date(from * 1_000);
    const endTime = new Date(to * 1_000);
    const sameDay = startTime.toDateString() === endTime.toDateString();

    if (sameDay) {
        return `${formatDate(startTime)} ${formatTime(startTime)} – ${formatTime(endTime)}`;
    }

    return `${formatDate(startTime)} ${formatTime(startTime)} – ${formatDate(endTime)} ${formatTime(endTime)}`;
}

export default function RangePicker({from, to, onApply, onClear}: Props) {
    const [open, setOpen] = useState(false);
    const [startInput, setStartInput] = useState("");
    const [endInput, setEndInput] = useState("");
    const ref = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!open) {
            return;
        }

        const handler = (event: MouseEvent) => {
            if (ref.current && !ref.current.contains(event.target as Node)) {
                setOpen(false);
            }
        };
        document.addEventListener("mousedown", handler);

        return () => document.removeEventListener("mousedown", handler);
    }, [open]);

    const handleApply = () => {
        const startTime = new Date(startInput).getTime() / 1_000;
        const endTime = new Date(endInput).getTime() / 1_000;

        if (!isNaN(startTime) && !isNaN(endTime) && startTime < endTime) {
            onApply(startTime, endTime);
            setOpen(false);
        }
    };

    const handlePreset = (seconds: number) => {
        const now = Math.floor(Date.now() / 1_000);
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
                    <Calendar className="size-3"/>
                    <span>{formatRange(from!, to!)}</span>
                    <X className="size-3 opacity-60 hover:opacity-100"/>
                </button>
            ) : (
                <button
                    onClick={() => setOpen(!open)}
                    className="inline-flex items-center justify-center size-7 rounded-md border border-border bg-card hover:bg-muted"
                    title="Custom range"
                >
                    <Calendar className="size-3.5"/>
                </button>
            )}

            {open && (
                <div className="absolute right-0 top-full mt-1 z-30 w-72 rounded-lg border bg-popover shadow-lg p-3 text-sm">
                    <div className="grid grid-cols-2 gap-1.5 mb-3">
                        {quickPresets.map(({label, seconds}) => (
                            <button
                                key={label}
                                onClick={() => handlePreset(seconds)}
                                className="px-2 py-1.5 rounded-md text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
                            >
                                {label}
                            </button>
                        ))}
                    </div>
                    <div className="space-y-2">
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
