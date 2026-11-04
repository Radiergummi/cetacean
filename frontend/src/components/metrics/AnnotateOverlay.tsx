import { type Gesture, type GestureEvent, type Mark, step } from "./annotateGesture";
import type { AnnotationScope } from "@/api/types";
import AnnotationForm from "@/components/annotations/AnnotationForm";
import { Popover, PopoverContent } from "@/components/ui/popover";
import { useClusterAnnotatable } from "@/hooks/useClusterAnnotatable";
import { type PointerEvent, useEffect, useReducer, useRef, useSyncExternalStore } from "react";

interface OverlayState {
  gesture: Gesture;

  /**
   * Where the gesture under way began, in epoch seconds, so a chart that
   * streams before it ends does not move its start.
   */
  anchor: number | null;

  /**
   * In epoch seconds, so the mark stays on its time while the chart streams.
   */
  mark: Mark | null;

  /** Counts marks, so each opens a fresh form. */
  marks: number;
}

const initialState: OverlayState = {
  gesture: { kind: "idle" },
  anchor: null,
  mark: null,
  marks: 0,
};

function began(gesture: Gesture) {
  return gesture.kind === "dragging"
    ? gesture.start
    : gesture.kind === "tapped"
      ? gesture.at
      : null;
}

// A reducer, not state read in the handler: pointer events can outrun a
// render, and each must build on the gesture the one before it left.
function overlayReducer(
  state: OverlayState,
  action: { event: GestureEvent; timeFor: (x: number) => number | null } | { type: "clear" },
): OverlayState {
  if ("type" in action) {
    return { ...state, mark: null };
  }

  const next = step(state.gesture, action.event);

  if (!next.mark && next.gesture === state.gesture) {
    return state;
  }

  if (!next.mark) {
    const x = began(next.gesture);
    const anchor =
      next.gesture.kind === state.gesture.kind
        ? state.anchor
        : x === null
          ? null
          : action.timeFor(x);

    return { ...state, gesture: next.gesture, anchor };
  }

  const from = state.anchor;
  const other =
    next.mark.to !== undefined && action.event.type === "up"
      ? action.timeFor(action.event.x)
      : null;

  if (from === null) {
    return { ...state, gesture: next.gesture, anchor: null, mark: null };
  }

  return {
    gesture: next.gesture,
    anchor: null,
    mark: other === null ? { from } : { from: Math.min(from, other), to: Math.max(from, other) },
    marks: state.marks + 1,
  };
}

/**
 * Takes the pointer over a chart while `capturing`, opening the form at the
 * mark it makes. `timeFor` turns a pixel into epoch seconds, `locate` back,
 * again whenever `subscribe` says the chart moved; `snap` moves a pixel to the
 * saved minute, and `onMarkOpen` lets the overlay outlive the mode.
 */
export default function AnnotateOverlay({
  timeFor,
  locate,
  subscribe,
  snap = (x) => x,
  scope,
  capturing,
  onMarkOpen,
  onDone,
}: {
  timeFor: (x: number) => number | null;
  locate: (time: number) => number | null;
  subscribe: (onMove: () => void) => () => void;
  snap?: ((x: number) => number) | undefined;
  scope: Exclude<AnnotationScope, "cluster">;
  capturing: boolean;
  onMarkOpen?: ((open: boolean) => void) | undefined;
  onDone: () => void;
}) {
  const [{ gesture, mark, marks }, dispatch] = useReducer(overlayReducer, initialState);
  const anchorRef = useRef<HTMLSpanElement>(null);
  const overlayRef = useRef<HTMLDivElement>(null);
  const clusterWide = useClusterAnnotatable();
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

  const send = (event: GestureEvent) => dispatch({ event, timeFor });

  const fromX = useSyncExternalStore(subscribe, () => (mark ? locate(mark.from) : null));
  const toX = useSyncExternalStore(subscribe, () =>
    mark?.to === undefined ? null : locate(mark.to),
  );

  // A start streamed off the chart holds at its left edge.
  const left = fromX === null ? 0 : snap(fromX);
  const right = toX === null ? left : snap(toX);
  const dragged =
    gesture.kind === "dragging"
      ? {
          left: snap(Math.min(gesture.start, gesture.current)),
          width:
            snap(Math.max(gesture.start, gesture.current)) -
            snap(Math.min(gesture.start, gesture.current)),
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
          // A Ctrl-click on macOS arrives as the secondary button.
          const primary = event.button === 0 || (event.button === 2 && event.ctrlKey);

          if (!capturing || !primary) {
            return;
          }

          event.currentTarget.setPointerCapture?.(event.pointerId);
          send({ type: "down", x: offset(event), touch: event.pointerType === "touch" });
        }}
        onPointerMove={(event) => send({ type: "move", x: offset(event) })}
        // Not gated on capturing: a drag outlives a mode released mid-drag.
        onPointerUp={(event) =>
          send({ type: "up", x: offset(event), touch: event.pointerType === "touch" })
        }
        onPointerCancel={() => send({ type: "cancel" })}
        // Ctrl-click opens the context menu on macOS; here it marks the chart.
        onContextMenu={(event) => {
          if (event.ctrlKey) {
            event.preventDefault();
          }
        }}
      >
        {dragged && (
          <div
            className="absolute inset-y-0 bg-annotation/15"
            data-testid="annotate-drag"
            style={dragged}
          />
        )}

        {gesture.kind === "tapped" && (
          <div
            className="absolute inset-y-0 w-px bg-annotation"
            style={{ left: snap(gesture.at) }}
          />
        )}

        <span
          ref={anchorRef}
          data-testid="annotate-mark"
          className="absolute inset-y-0 data-point:w-px data-point:bg-annotation data-range:bg-annotation/15"
          data-point={mark && fromX !== null && right <= left ? "" : undefined}
          data-range={mark && right > left ? "" : undefined}
          style={{ left, width: right > left ? right - left : undefined }}
        />
      </div>

      <Popover
        open={mark !== null}
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
          {mark && (
            <AnnotationForm
              key={marks}
              scopes={clusterWide ? [scope, "cluster"] : [scope]}
              initial={mark}
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
