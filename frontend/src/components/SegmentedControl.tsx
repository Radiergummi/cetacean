import { MenuContent, MenuRadioItem } from "@/components/ui/menu";
// oxlint-disable react/refs -- `close` only dereferences the menu actions ref
// when invoked as an event handler. The rule flags it because the callback is
// handed to an `overflowContent` render prop, but the ref is never read during
// render.
import { cn } from "@/lib/utils";
import { Menu } from "@base-ui/react/menu";
import { Toggle } from "@base-ui/react/toggle";
import { ToggleGroup } from "@base-ui/react/toggle-group";
import { ChevronDown } from "lucide-react";
import type { ReactNode } from "react";
import { useRef } from "react";

/** Matches the Button scale: `default` is h-8, `sm` is h-7. */
type SegmentedControlSize = "default" | "sm";

const sizes = {
  default: {
    frame: "h-8",
    segment: "gap-1.5 px-3 py-1 text-sm",
    overflow: "gap-1 px-2 py-1 text-sm",
  },
  sm: {
    frame: "h-7",
    segment: "gap-1 px-2.5 py-0.5 text-[0.8rem]",
    overflow: "gap-1 px-1.5 py-0.5 text-[0.8rem]",
  },
} as const;

export interface Segment<T extends string> {
  value: T;
  label: string;
  /**
   * Shown in place of the label, which stays the segment's accessible name.
   */
  icon?: ReactNode | undefined;
  badge?: number | undefined;
  disabled?: boolean | undefined;
}

export default function SegmentedControl<T extends string>({
  segments,
  value,
  onChange,
  max = 5,
  size = "default",
  overflowIcon,
  overflowLabel,
  overflowName = "More options",
  overflowActive,
  overflowContent,
  className,
}: {
  segments: Segment<T>[];
  value: T;
  onChange: (value: T) => void;
  max?: number | undefined;
  size?: SegmentedControlSize | undefined;
  /** Replace the default chevron icon on the overflow button. */
  overflowIcon?: ReactNode | undefined;
  /** Label shown next to the icon when the overflow is active. */
  overflowLabel?: ReactNode | undefined;
  /** Accessible name for the overflow button while it shows no label. */
  overflowName?: string | undefined;
  /** Whether the overflow button should appear in the active style. */
  overflowActive?: boolean | undefined;
  /** Custom popover content. Receives a `close` callback. When provided, the overflow button is always shown. */
  overflowContent?: ((close: () => void) => ReactNode) | undefined;
  /** Merged into the frame, as for a height that matches its neighbours. */
  className?: string | undefined;
}) {
  const actionsRef = useRef<Menu.Root.Actions>(null);

  const visible = segments.slice(0, max);
  const overflow = segments.slice(max);
  const activeOverflow = overflow.find((segment) => segment.value === value);
  const hasOverflow = overflow.length > 0 || overflowContent != null;
  const isActive = overflowActive ?? !!activeOverflow;

  const close = () => actionsRef.current?.close();
  const scale = sizes[size];

  return (
    <div
      className={cn(
        "inline-flex items-stretch gap-0.5 rounded-md bg-card p-0.5 ring-1 ring-input ring-inset",
        scale.frame,
        className,
      )}
    >
      <ToggleGroup
        value={[value]}
        onValueChange={(values) => {
          if (values.length > 0) {
            onChange(values[0] as T);
          }
        }}
        className="flex items-stretch gap-0.5"
      >
        {visible.map(({ badge, disabled, icon, label, value: segmentValue }) => (
          <Toggle
            key={segmentValue}
            value={segmentValue}
            disabled={disabled}
            aria-label={icon ? label : undefined}
            title={icon ? label : undefined}
            className={cn(
              "group/seg inline-flex cursor-pointer items-center rounded-sm font-medium text-muted-foreground transition outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-default disabled:text-muted-foreground/40 data-pressed:bg-primary data-pressed:text-primary-foreground data-pressed:shadow-sm",
              scale.segment,
            )}
          >
            {icon ?? <span>{label}</span>}
            {badge != null && (
              <span className="min-size-4 inline-flex items-center justify-center rounded-full bg-foreground/5 px-1 text-[10px] font-semibold tabular-nums group-data-pressed/seg:bg-accent/25">
                {badge}
              </span>
            )}
          </Toggle>
        ))}
      </ToggleGroup>

      {hasOverflow && (
        <Menu.Root
          modal={false}
          actionsRef={actionsRef}
        >
          <Menu.Trigger
            aria-current={isActive || undefined}
            aria-label={overflowLabel || activeOverflow ? undefined : overflowName}
            className={cn(
              "inline-flex cursor-pointer items-center rounded-sm text-muted-foreground transition outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 aria-current:bg-primary aria-current:text-primary-foreground aria-current:shadow-sm",
              scale.overflow,
            )}
          >
            {overflowLabel ?? (activeOverflow ? <span>{activeOverflow.label}</span> : undefined)}
            {overflowIcon ?? <ChevronDown className="size-3" />}
          </Menu.Trigger>

          <MenuContent>
            {overflowContent
              ? overflowContent(close)
              : overflow.length > 0 && (
                  <Menu.RadioGroup
                    value={value}
                    onValueChange={(nextValue) => {
                      onChange(nextValue as T);
                    }}
                  >
                    {overflow.map(({ badge, disabled, label, value: segmentValue }) => (
                      <MenuRadioItem
                        key={segmentValue}
                        value={segmentValue}
                        disabled={disabled}
                        closeOnClick
                        className="data-disabled:text-muted-foreground/40"
                      >
                        <span>{label}</span>
                        {badge != null && (
                          <span className="text-xs text-muted-foreground tabular-nums">
                            {badge}
                          </span>
                        )}
                      </MenuRadioItem>
                    ))}
                  </Menu.RadioGroup>
                )}
          </MenuContent>
        </Menu.Root>
      )}
    </div>
  );
}
