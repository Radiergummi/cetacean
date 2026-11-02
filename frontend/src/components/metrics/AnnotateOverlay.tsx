import { type Gesture, type GestureEvent, type Mark, step } from "./annotateGesture";
import type { AnnotationScope } from "@/api/types";
import AnnotationForm from "@/components/annotations/AnnotationForm";
import { Popover, PopoverContent } from "@/components/ui/popover";
import { useAnnotationCapabilities } from "@/hooks/useAnnotationCapabilities";
import { type PointerEvent, useRef, useState } from "react";

const idle: Gesture = { kind: "idle" };

/**
 * Takes the pointer over a chart in annotate mode, so zoom, isolation and the
 * tooltip never see the gesture, and opens the form at the mark it makes.
 * `timeFor` turns a pixel into epoch seconds.
 */
export default function AnnotateOverlay({
  timeFor,
  scope,
  onDone,
}: {
  timeFor: (x: number) => number | null;
  scope: Exclude<AnnotationScope, "cluster">;
  onDone: () => void;
}) {
  const [gesture, setGesture] = useState<Gesture>(idle);
  const [mark, setMark] = useState<(Mark & { key: number }) | null>(null);
  const anchorRef = useRef<HTMLSpanElement>(null);
  const { clusterWide } = useAnnotationCapabilities();

  const feed = (event: GestureEvent) => {
    const next = step(gesture, event);

    setGesture(next.gesture);

    if (next.mark) {
      setMark({ ...next.mark, key: Date.now() });
    }
  };

  const offset = (event: PointerEvent<HTMLDivElement>) =>
    event.clientX - event.currentTarget.getBoundingClientRect().left;

  const from = mark ? timeFor(mark.from) : null;
  const to = mark?.to === undefined ? undefined : (timeFor(mark.to) ?? undefined);
  const dragged =
    gesture.kind === "dragging"
      ? {
          left: Math.min(gesture.start, gesture.current),
          width: Math.abs(gesture.current - gesture.start),
        }
      : null;

  return (
    <div
      data-testid="annotate-overlay"
      className="absolute inset-0 z-10 cursor-crosshair touch-none"
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture?.(event.pointerId);
        feed({ type: "down", x: offset(event), touch: event.pointerType === "touch" });
      }}
      onPointerMove={(event) => feed({ type: "move", x: offset(event) })}
      onPointerUp={(event) =>
        feed({ type: "up", x: offset(event), touch: event.pointerType === "touch" })
      }
      onPointerCancel={() => feed({ type: "cancel" })}
    >
      {dragged && (
        <div
          className="absolute inset-y-0 bg-annotation/15"
          style={dragged}
        />
      )}

      {gesture.kind === "tapped" && (
        <div
          className="absolute inset-y-0 w-px bg-annotation"
          style={{ left: gesture.at }}
        />
      )}

      <span
        ref={anchorRef}
        className="absolute top-0 h-full"
        style={{
          left: mark?.from ?? 0,
          width: mark?.to === undefined ? 0 : mark.to - mark.from,
        }}
      />

      <Popover
        open={mark !== null && from !== null}
        onOpenChange={(open) => {
          if (!open) {
            setMark(null);
          }
        }}
      >
        <PopoverContent
          anchor={anchorRef}
          className="w-80"
        >
          {mark && from !== null && (
            <AnnotationForm
              key={mark.key}
              scopes={clusterWide ? [scope, "cluster"] : [scope]}
              initial={{ from, to }}
              onDone={() => {
                setMark(null);
                onDone();
              }}
            />
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}
