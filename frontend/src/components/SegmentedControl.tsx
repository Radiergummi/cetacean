import { ChevronDown } from "lucide-react";
import type { ReactNode } from "react";
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
      className="group/seg inline-flex cursor-pointer items-center gap-1.5 rounded-sm px-3 py-1 text-sm font-medium text-muted-foreground transition hover:text-foreground disabled:cursor-default disabled:text-muted-foreground/40 aria-current:bg-primary aria-current:text-primary-foreground aria-current:shadow-sm"
    >
      <span>{label}</span>
      {badge != null && (
        <span className="min-size-4 inline-flex items-center justify-center rounded-full bg-foreground/5 px-1 text-[10px] font-semibold tabular-nums group-aria-current/seg:bg-accent/25">
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
  overflowIcon,
  overflowLabel,
  overflowActive,
  overflowContent,
}: {
  segments: Segment<T>[];
  value: T;
  onChange: (value: T) => void;
  max?: number;
  /** Replace the default chevron icon on the overflow button. */
  overflowIcon?: ReactNode;
  /** Label shown next to the icon when the overflow is active. */
  overflowLabel?: ReactNode;
  /** Whether the overflow button should appear in the active style. */
  overflowActive?: boolean;
  /** Custom popover content. Receives a `close` callback. When provided, the overflow button is always shown. */
  overflowContent?: (close: () => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) {
      return;
    }

    const handleClick = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
      }
    };

    document.addEventListener("mousedown", handleClick);
    document.addEventListener("keydown", handleKeyDown);

    return () => {
      document.removeEventListener("mousedown", handleClick);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  const visible = segments.slice(0, max);
  const overflow = segments.slice(max);
  const activeOverflow = overflow.find((segment) => segment.value === value);
  const hasOverflow = overflow.length > 0 || overflowContent != null;
  const isActive = overflowActive ?? !!activeOverflow;

  const close = () => setOpen(false);

  return (
    <div className="inline-flex h-8 items-center gap-0.5 rounded-md bg-card px-0.5 ring-1 ring-input ring-inset">
      {visible.map((segment) => (
        <SegmentButton
          key={segment.value}
          segment={segment}
          active={value === segment.value}
          onClick={() => onChange(segment.value)}
        />
      ))}

      {hasOverflow && (
        <div
          className="relative"
          ref={menuRef}
        >
          <button
            onClick={() => setOpen((o) => !o)}
            aria-current={isActive || undefined}
            aria-haspopup="menu"
            aria-expanded={open}
            className="inline-flex cursor-pointer items-center gap-1 rounded-sm px-2 py-1 text-sm text-muted-foreground transition hover:text-foreground aria-current:bg-primary aria-current:text-primary-foreground aria-current:shadow-sm"
          >
            {overflowLabel ?? (activeOverflow ? <span>{activeOverflow.label}</span> : undefined)}
            {overflowIcon ?? <ChevronDown className="size-3" />}
          </button>

          {open && (
            <div
              role="menu"
              className="absolute top-full right-0 z-50 mt-1 min-w-36 rounded-md border bg-popover p-1 shadow-md"
            >
              {overflowContent
                ? overflowContent(close)
                : overflow.map(({ badge, disabled, label, value: segmentValue }) => (
                    <button
                      key={segmentValue}
                      role="menuitemradio"
                      disabled={disabled}
                      aria-checked={value === segmentValue}
                      onClick={() => {
                        onChange(segmentValue);
                        setOpen(false);
                      }}
                      className="flex w-full items-center justify-between gap-3 rounded-sm px-2 py-1.5 text-sm text-popover-foreground hover:bg-accent hover:text-accent-foreground disabled:cursor-default disabled:text-muted-foreground/40 disabled:hover:bg-transparent aria-checked:bg-accent aria-checked:text-accent-foreground"
                    >
                      <span>{label}</span>
                      {badge != null && (
                        <span className="text-xs text-muted-foreground tabular-nums">{badge}</span>
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
