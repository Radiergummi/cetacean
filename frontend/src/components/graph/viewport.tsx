import { Controls, ControlButton, useReactFlow, useStore, type Viewport } from "@xyflow/react";
import { Fullscreen, Minimize, Undo2, ZoomIn, ZoomOut } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";

const fitViewOptions = { padding: 0.15 };

// No overshoot: the viewport is clamped, so a bounce would be cut short. Asked
// for reduced motion, the viewport still lands where it should, at once.
export function glide(extra?: { zoom: number }) {
  return {
    ...fitViewOptions,
    ...extra,
    ease: (t: number) => 1 - (1 - t) ** 3,
    interpolate: "smooth" as const,
    duration: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? 0 : 320,
  };
}

/** A viewport held outside the canvas, so it outlives a remount. */
export function useKeptViewport() {
  const kept = useRef<Viewport | null>(null);

  return useMemo(
    () => ({
      keep: (moved: Viewport) => {
        kept.current = moved;
      },
      take: () => kept.current,
    }),
    [],
  );
}

export type KeptViewport = ReturnType<typeof useKeptViewport>;

/**
 * React Flow is an editor by default: every node and edge is a tab stop, told
 * through a description offering to delete it. None of that is true here, and
 * the content is focusable in its own right.
 */
export const readOnlyKeyboard = {
  nodesConnectable: false,
  nodesFocusable: false,
  edgesFocusable: false,
  disableKeyboardA11y: true,
} as const;

/**
 * Refits when the frame changes size — a window, a sidebar, full screen.
 * Without it the graph keeps a viewport fitted to a frame that is gone, with
 * nothing but the reset button to say so.
 */
export function FitOnResize() {
  const { fitView } = useReactFlow();
  const width = useStore((state) => state.width);
  const height = useStore((state) => state.height);

  useEffect(() => {
    if (width && height) {
      void fitView({ ...glide(), duration: 0 });
    }
  }, [width, height, fitView]);

  return null;
}

// React Flow's own control CSS loads after Tailwind and carries one hardcoded
// light palette: it is why a class cannot colour these buttons, and why they
// stayed white on a dark page. Its own variables take the theme instead.
const controlStyle = {
  boxShadow: "none",
  "--xy-controls-button-background-color": "var(--color-card)",
  "--xy-controls-button-background-color-hover": "var(--color-accent)",
  "--xy-controls-button-color": "var(--color-muted-foreground)",
  "--xy-controls-button-color-hover": "var(--color-foreground)",
  "--xy-controls-button-border-color": "transparent",
} as CSSProperties;

// The same stylesheet fills an icon at 12px, which turns a stroked Lucide
// glyph into a solid blob.
const controlIcon = { fill: "none", width: 16, height: 16, maxWidth: "none", maxHeight: "none" };

/** Zoom, reset and full screen, over whichever graph encloses it. */
export function GraphControls() {
  const { fitView, zoomIn, zoomOut } = useReactFlow();
  const canvas = useStore((state) => state.domNode);
  const [fullscreen, setFullscreen] = useState(false);

  useEffect(() => {
    const onChange = () => setFullscreen(document.fullscreenElement === canvas);

    document.addEventListener("fullscreenchange", onChange);

    return () => document.removeEventListener("fullscreenchange", onChange);
  }, [canvas]);

  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else {
      void canvas?.requestFullscreen();
    }
  }, [canvas]);

  return (
    <Controls
      position="bottom-right"
      orientation="horizontal"
      showZoom={false}
      showFitView={false}
      showInteractive={false}
      className="overflow-hidden rounded-md border bg-card"
      style={controlStyle}
    >
      <ControlButton
        onClick={() => zoomIn(glide())}
        title="Zoom in"
        aria-label="Zoom in"
      >
        <ZoomIn style={controlIcon} />
      </ControlButton>

      <ControlButton
        onClick={() => zoomOut(glide())}
        title="Zoom out"
        aria-label="Zoom out"
      >
        <ZoomOut style={controlIcon} />
      </ControlButton>

      <ControlButton
        onClick={() => {
          void fitView(glide());
        }}
        title="Reset view"
        aria-label="Reset view"
      >
        <Undo2 style={controlIcon} />
      </ControlButton>

      <ControlButton
        onClick={toggleFullscreen}
        title={fullscreen ? "Exit full screen" : "Full screen"}
        aria-label={fullscreen ? "Exit full screen" : "Full screen"}
      >
        {fullscreen ? <Minimize style={controlIcon} /> : <Fullscreen style={controlIcon} />}
      </ControlButton>
    </Controls>
  );
}
