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
  // Pointer events can outrun a render, so each builds on the last one's
  // gesture rather than on the one this render saw.
  const gestureRef = useRef<Gesture>(idle);
  const [mark, setMark] = useState<(Mark & { key: number }) | null>(null);
  const anchorRef = useRef<HTMLSpanElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);
  const { clusterWide } = useAnnotationCapabilities();

  const feed = (event: GestureEvent) => {
    const next = step(gestureRef.current, event);

    gestureRef.current = next.gesture;
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
      ref={overlayRef}
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
        data-testid="annotate-mark"
        className="absolute inset-y-0 data-point:w-px data-point:bg-annotation data-range:bg-annotation/15"
        data-point={mark && mark.to === undefined ? "" : undefined}
        data-range={mark?.to === undefined ? undefined : ""}
        style={{
          left: mark?.from ?? 0,
          width: mark?.to === undefined ? undefined : mark.to - mark.from,
        }}
      />

      <Popover
        open={mark !== null && from !== null}
        onOpenChange={(open, { event, reason }) => {
          // A press on the chart makes the next mark; it must not also dismiss it.
          const onChart =
            reason === "outside-press" &&
            event.target instanceof Node &&
            overlayRef.current?.contains(event.target);

          if (!open && !onChart) {
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
