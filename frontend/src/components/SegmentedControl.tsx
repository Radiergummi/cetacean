export interface Segment<T extends string> {
    value: T;
    label: string;
    badge?: number;
}

export default function SegmentedControl<T extends string>(
    {
        segments,
        value,
        onChange,
    }: {
        segments: Segment<T>[];
        value: T;
        onChange: (value: T) => void;
    },
) {
    return (
        <div className="inline-flex h-8 px-0.5 items-center gap-0.5 rounded-md bg-muted ring-1 ring-input ring-inset">
            {segments.map(({badge, label, value: item}) => (
                <button
                    key={item}
                    onClick={() => onChange(item)}
                    className={`inline-flex items-center gap-1.5 rounded-[calc(var(--radius)*0.65)] px-3 py-1 text-sm font-medium transition ${
                        value === item
                            ? "bg-primary text-white shadow-sm"
                            : "text-muted-foreground hover:text-foreground"
                    }`}
                >
                    <span>{label}</span>
                    {badge != null && (
                        <span
                            className={`inline-flex items-center justify-center min-w-4 h-4 px-1 rounded-full text-[10px] font-semibold tabular-nums ${
                                value === item ? "bg-accent/25" : "bg-foreground/5"
                            }`}
                        >
                            {badge}
                        </span>
                    )}
                </button>
            ))}
        </div>
    );
}
