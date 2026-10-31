// oxlint-disable react/refs -- the tooltip position is read through a ref by
// an imperative Chart.js tooltip handler, which runs outside React's render.
import { chartTooltipClasses } from "@/lib/chartTooltip.ts";
import { useRef } from "react";

export interface TooltipMarkers {
  changes: { key: number; time: string; text: string }[];
  more: number;
  gap: { title: string; detail: string } | undefined;
}

export interface TooltipData {
  time: string;
  series: { label: string; color: string; value: string; raw: number; dashed?: boolean }[];
  x: number;
  chartWidth: number;
  top: number;
  markers?: TooltipMarkers | undefined;
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

          {tooltip.markers && (
            <div className="mt-1.5 flex flex-col gap-1 border-t pt-1.5">
              {tooltip.markers.gap && (
                <div>
                  <div className="font-semibold text-foreground">{tooltip.markers.gap.title}</div>
                  <div className="text-muted-foreground">{tooltip.markers.gap.detail}</div>
                </div>
              )}

              {tooltip.markers.changes.map(({ key, text, time }) => (
                <div
                  key={key}
                  className="flex items-center gap-2 whitespace-nowrap"
                >
                  <span className="text-muted-foreground">{time}</span>
                  <span className="text-foreground">{text}</span>
                </div>
              ))}

              {tooltip.markers.more > 0 && (
                <div className="text-muted-foreground">{tooltip.markers.more} more</div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
