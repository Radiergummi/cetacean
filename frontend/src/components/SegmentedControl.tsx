import { Menu } from "@base-ui/react/menu";
import { Toggle } from "@base-ui/react/toggle";
import { ToggleGroup } from "@base-ui/react/toggle-group";
import { ChevronDown } from "lucide-react";
import type { ReactNode } from "react";
import { useRef } from "react";

export interface Segment<T extends string> {
  value: T;
  label: string;
  badge?: number;
  disabled?: boolean;
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
  const actionsRef = useRef<Menu.Root.Actions>(null);

  const visible = segments.slice(0, max);
  const overflow = segments.slice(max);
  const activeOverflow = overflow.find((segment) => segment.value === value);
  const hasOverflow = overflow.length > 0 || overflowContent != null;
  const isActive = overflowActive ?? !!activeOverflow;

  const close = () => actionsRef.current?.close();

  return (
    <div className="inline-flex h-8 items-center gap-0.5 rounded-md bg-card px-0.5 ring-1 ring-input ring-inset">
      <ToggleGroup
        value={[value]}
        onValueChange={(values) => {
          if (values.length > 0) {
            onChange(values[0] as T);
          }
        }}
        className="flex items-center gap-0.5"
      >
        {visible.map(({ badge, disabled, label, value: segmentValue }) => (
          <Toggle
            key={segmentValue}
            value={segmentValue}
            disabled={disabled}
            className="group/seg inline-flex cursor-pointer items-center gap-1.5 rounded-sm px-3 py-1 text-sm font-medium text-muted-foreground outline-none transition hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-default disabled:text-muted-foreground/40 data-pressed:bg-primary data-pressed:text-primary-foreground data-pressed:shadow-sm"
          >
            <span>{label}</span>
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
            className="inline-flex cursor-pointer items-center gap-1 rounded-sm px-2 py-1 text-sm text-muted-foreground outline-none transition hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 aria-current:bg-primary aria-current:text-primary-foreground aria-current:shadow-sm"
          >
            {overflowLabel ?? (activeOverflow ? <span>{activeOverflow.label}</span> : undefined)}
            {overflowIcon ?? <ChevronDown className="size-3" />}
          </Menu.Trigger>

          <Menu.Portal>
            <Menu.Positioner
              align="end"
              sideOffset={4}
              className="z-50"
            >
              <Menu.Popup className="min-w-36 rounded-md border bg-popover p-1 shadow-md outline-hidden">
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
                          <Menu.RadioItem
                            key={segmentValue}
                            value={segmentValue}
                            disabled={disabled}
                            closeOnClick
                            className="flex w-full cursor-default items-center justify-between gap-3 rounded-sm px-2 py-1.5 text-sm text-popover-foreground outline-none data-highlighted:bg-accent data-highlighted:text-accent-foreground data-disabled:pointer-events-none data-disabled:text-muted-foreground/40 data-checked:bg-accent data-checked:text-accent-foreground"
                          >
                            <span>{label}</span>
                            {badge != null && (
                              <span className="text-xs text-muted-foreground tabular-nums">
                                {badge}
                              </span>
                            )}
                          </Menu.RadioItem>
                        ))}
                      </Menu.RadioGroup>
                    )}
              </Menu.Popup>
            </Menu.Positioner>
          </Menu.Portal>
        </Menu.Root>
      )}
    </div>
  );
}
