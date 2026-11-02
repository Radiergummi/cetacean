import { type Gesture, type GestureEvent, type Mark, step } from "./annotateGesture";
import type { AnnotationScope } from "@/api/types";
import AnnotationForm from "@/components/annotations/AnnotationForm";
import { Popover, PopoverContent } from "@/components/ui/popover";
import { useAnnotationCapabilities } from "@/hooks/useAnnotationCapabilities";
import { type PointerEvent, useEffect, useReducer, useRef } from "react";

interface OverlayState {
  gesture: Gesture;
  mark: Mark | null;

  /** Counts marks, so each opens a fresh form. */
  marks: number;
}

const initialState: OverlayState = { gesture: { kind: "idle" }, mark: null, marks: 0 };

// A reducer, not state read in the handler: pointer events can outrun a
// render, and each must build on the gesture the one before it left.
function overlayReducer(state: OverlayState, action: GestureEvent | { type: "clear" }) {
  if (action.type === "clear") {
    return { ...state, mark: null };
  }

  const next = step(state.gesture, action);

  return next.mark
    ? { gesture: next.gesture, mark: next.mark, marks: state.marks + 1 }
    : { ...state, gesture: next.gesture };
}

/**
 * Takes the pointer over a chart while `capturing`, opening the form at the
 * mark it makes. `timeFor` turns a pixel into epoch seconds and `snap` moves
 * it to the saved minute; `onMarkOpen` lets the overlay outlive the mode.
 */
export default function AnnotateOverlay({
  timeFor,
  snap = (x) => x,
  scope,
  capturing,
  onMarkOpen,
  onDone,
}: {
  timeFor: (x: number) => number | null;
  snap?: ((x: number) => number) | undefined;
  scope: Exclude<AnnotationScope, "cluster">;
  capturing: boolean;
  onMarkOpen?: ((open: boolean) => void) | undefined;
  onDone: () => void;
}) {
  const [{ gesture, mark, marks }, dispatch] = useReducer(overlayReducer, initialState);
  const anchorRef = useRef<HTMLSpanElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);
  const { clusterWide } = useAnnotationCapabilities();
  const markOpen = mark !== null;

  useEffect(() => {
    if (!markOpen) {
      return;
    }

    onMarkOpen?.(true);

    return () => onMarkOpen?.(false);
  }, [markOpen, onMarkOpen]);

  const offset = (event: PointerEvent<HTMLDivElement>) =>
    event.clientX - event.currentTarget.getBoundingClientRect().left;

  const from = mark ? timeFor(mark.from) : null;
  const left = mark ? snap(mark.from) : 0;
  const right = mark?.to === undefined ? left : snap(mark.to);
  const to = mark?.to === undefined ? undefined : (timeFor(mark.to) ?? undefined);
  const dragged =
    gesture.kind === "dragging"
      ? {
          left: Math.min(gesture.start, gesture.current),
          width: Math.abs(gesture.current - gesture.start),
        }
      : null;

  return (
    <>
      <div
        ref={overlayRef}
        data-testid="annotate-overlay"
        data-capturing={capturing || undefined}
        className="pointer-events-none absolute inset-0 z-10 data-capturing:pointer-events-auto data-capturing:cursor-crosshair data-capturing:touch-none"
        onPointerDown={(event) => {
          if (!capturing) {
            return;
          }

          event.currentTarget.setPointerCapture?.(event.pointerId);
          dispatch({ type: "down", x: offset(event), touch: event.pointerType === "touch" });
        }}
        onPointerMove={(event) => {
          if (gesture.kind === "dragging") {
            dispatch({ type: "move", x: offset(event) });
          }
        }}
        onPointerUp={(event) => {
          if (capturing) {
            dispatch({ type: "up", x: offset(event), touch: event.pointerType === "touch" });
          }
        }}
        onPointerCancel={() => dispatch({ type: "cancel" })}
        // Ctrl-click opens the context menu on macOS; here it marks the chart.
        onContextMenu={(event) => event.preventDefault()}
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
          data-point={mark && right <= left ? "" : undefined}
          data-range={mark && right > left ? "" : undefined}
          style={{ left, width: right > left ? right - left : undefined }}
        />
      </div>

      <Popover
        open={mark !== null && from !== null}
        onOpenChange={(open, { event, reason }) => {
          // A press on the chart makes the next mark; it must not also dismiss it.
          const onChart =
            reason === "outside-press" &&
            event.target instanceof Node &&
            overlayRef.current?.contains(event.target);

          if (!open && !onChart) {
            dispatch({ type: "clear" });
          }
        }}
      >
        <PopoverContent
          anchor={anchorRef}
          className="w-80 shadow-xl ring-foreground/15"
        >
          {mark && from !== null && (
            <AnnotationForm
              key={marks}
              scopes={clusterWide ? [scope, "cluster"] : [scope]}
              initial={{ from, to }}
              onDone={() => {
                dispatch({ type: "clear" });
                onDone();
              }}
            />
          )}
        </PopoverContent>
      </Popover>
    </>
  );
}
