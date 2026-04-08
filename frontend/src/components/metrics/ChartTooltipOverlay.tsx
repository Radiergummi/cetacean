import { chartTooltipClasses } from "@/lib/chartTooltip.ts";
import { useRef } from "react";

export interface TooltipData {
  time: string;
  series: { label: string; color: string; value: string; raw: number; dashed?: boolean }[];
  x: number;
  chartWidth: number;
  top: number;
}

const tooltipGap = 20;

function tooltipLeft({ chartWidth, x }: TooltipData, element: HTMLDivElement | null): number {
  const width = element?.offsetWidth ?? 0;
  const showLeft = x > chartWidth / 2;

  if (showLeft) {
    return x - width - tooltipGap;
  }

  return x + tooltipGap;
}

interface Props {
  tooltip: TooltipData | null;
  visible: boolean;
}

export default function ChartTooltipOverlay({ tooltip, visible }: Props) {
  const elementRef = useRef<HTMLDivElement>(null);

  return (
    <div
      ref={elementRef}
      className={chartTooltipClasses}
      style={{
        left: tooltip ? tooltipLeft(tooltip, elementRef.current) : 0,
        top: tooltip?.top ?? 0,
        opacity: tooltip && visible ? 1 : 0,
        transition: "opacity 100ms ease",
      }}
    >
      {tooltip && (
        <>
          <div className="mb-1.5 font-semibold text-foreground">{tooltip.time}</div>
          {tooltip.series.map(({ color, dashed, label, value }) => (
            <div
              key={label}
              className="flex items-center gap-2 whitespace-nowrap"
            >
              {dashed ? (
                <span
                  className="w-3 shrink-0 border-t-2 border-dashed"
                  style={{ borderColor: color }}
                />
              ) : (
                <span
                  className="h-3 w-1 shrink-0 rounded-sm"
                  style={{ background: color }}
                />
              )}

              <span className="text-muted-foreground">{label}</span>
              <span className="ms-auto ps-4 font-semibold text-foreground">{value}</span>
            </div>
          ))}
        </>
      )}
    </div>
  );
}
