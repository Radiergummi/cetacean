interface Props {
  label: string;
  value: number | null; // 0-100 percentage
  subtitle?: string;
  size?: "sm" | "md";
}

const SIZES = {
  sm: { size: 56, stroke: 5, text: "text-xs", label: "text-[9px]" },
  md: { size: 96, stroke: 8, text: "text-lg", label: "text-xs" },
};

function colorForValue(v: number): string {
  if (v >= 90) return "#ef4444"; // red
  if (v >= 75) return "#f59e0b"; // amber
  return "#10b981"; // emerald
}

export default function ResourceGauge({ label, value, subtitle, size = "md" }: Props) {
  const s = SIZES[size];
  const radius = (s.size - s.stroke) / 2;
  const circumference = Math.PI * radius;
  const pct = value != null ? Math.max(0, Math.min(100, value)) : 0;
  const offset = circumference - (pct / 100) * circumference;
  const color = value != null ? colorForValue(pct) : "var(--color-muted)";

  return (
    <div className="flex flex-col items-center gap-0.5">
      <div className="relative" style={{ width: s.size, height: s.size / 2 + s.stroke }}>
        <svg
          width={s.size}
          height={s.size / 2 + s.stroke}
          viewBox={`0 0 ${s.size} ${s.size / 2 + s.stroke}`}
        >
          <path
            d={describeArc(s.size / 2, s.size / 2, radius)}
            fill="none"
            stroke="var(--color-muted)"
            strokeWidth={s.stroke}
            strokeLinecap="round"
          />
          {value != null && (
            <path
              d={describeArc(s.size / 2, s.size / 2, radius)}
              fill="none"
              stroke={color}
              strokeWidth={s.stroke}
              strokeLinecap="round"
              strokeDasharray={circumference}
              strokeDashoffset={offset}
              className="transition-all duration-700 ease-out"
            />
          )}
        </svg>
        <div className="absolute inset-0 flex items-end justify-center pb-0.5">
          <span className={`${s.text} font-semibold tabular-nums leading-none`}>
            {value != null ? `${Math.round(pct)}%` : "\u2014"}
          </span>
        </div>
      </div>
      <span className={`${s.label} font-medium text-muted-foreground`}>{label}</span>
      {subtitle && <span className="text-[10px] text-muted-foreground/70 -mt-0.5">{subtitle}</span>}
    </div>
  );
}

function describeArc(cx: number, cy: number, r: number): string {
  return `M ${cx - r} ${cy} A ${r} ${r} 0 0 1 ${cx + r} ${cy}`;
}
