import { ChevronDown } from "lucide-react";
import { useEffect, useRef, useState } from "react";

export interface Segment<T extends string> {
  value: T;
  label: string;
  badge?: number;
  disabled?: boolean;
}

function SegmentButton<T extends string>({
  segment,
  active,
  onClick,
}: {
  segment: Segment<T>;
  active: boolean;
  onClick: () => void;
}) {
  const { badge, disabled, label } = segment;
  return (
    <button
      disabled={disabled}
      onClick={onClick}
      aria-current={active || undefined}
      className="group/seg inline-flex items-center gap-1.5 rounded-sm px-3 py-1 text-sm font-medium transition cursor-pointer text-muted-foreground hover:text-foreground disabled:text-muted-foreground/40 disabled:cursor-default aria-current:bg-primary aria-current:text-primary-foreground aria-current:shadow-sm"
    >
      <span>{label}</span>
      {badge != null && (
        <span className="inline-flex items-center justify-center min-size-4 px-1 rounded-full text-[10px] font-semibold tabular-nums bg-foreground/5 group-aria-current/seg:bg-accent/25">
          {badge}
        </span>
      )}
    </button>
  );
}

export default function SegmentedControl<T extends string>({
  segments,
  value,
  onChange,
  max = 5,
}: {
  segments: Segment<T>[];
  value: T;
  onChange: (value: T) => void;
  max?: number;
}) {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) {
      return;
    }
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const visible = segments.slice(0, max);
  const overflow = segments.slice(max);
  const activeOverflow = overflow.find((segment) => segment.value === value);

  return (
    <div className="inline-flex h-8 px-0.5 items-center gap-0.5 rounded-md bg-card ring-1 ring-input ring-inset">
      {visible.map((segment) => (
        <SegmentButton
          key={segment.value}
          segment={segment}
          active={value === segment.value}
          onClick={() => onChange(segment.value)}
        />
      ))}

      {overflow.length > 0 && (
        <div className="relative" ref={menuRef}>
          <button
            onClick={() => setOpen((o) => !o)}
            aria-current={activeOverflow ? true : undefined}
            className="inline-flex items-center px-2 py-2 rounded-sm cursor-pointer transition gap-1 text-muted-foreground hover:text-foreground aria-current:bg-primary aria-current:text-primary-foreground aria-current:shadow-sm"
          >
            {activeOverflow ? <span>{activeOverflow.label}</span> : undefined}
            <ChevronDown className="size-3" />
          </button>

          {open && (
            <div className="absolute right-0 top-full mt-1 z-50 min-w-36 rounded-md border bg-popover p-1 shadow-md">
              {overflow.map(({ badge, disabled, label, value: segmentValue }) => (
                <button
                  key={segmentValue}
                  disabled={disabled}
                  aria-selected={value === segmentValue || undefined}
                  onClick={() => {
                    onChange(segmentValue);
                    setOpen(false);
                  }}
                  className="flex w-full items-center justify-between gap-3 rounded-sm px-2 py-1.5 text-sm text-popover-foreground hover:bg-accent hover:text-accent-foreground disabled:text-muted-foreground/40 disabled:cursor-default disabled:hover:bg-transparent aria-selected:bg-accent aria-selected:text-accent-foreground"
                >
                  <span>{label}</span>
                  {badge != null && (
                    <span className="text-xs tabular-nums text-muted-foreground">{badge}</span>
                  )}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
