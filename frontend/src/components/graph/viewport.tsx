import { Controls, ControlButton, useReactFlow, useStore } from "@xyflow/react";
import { Fullscreen, Minimize, Undo2, ZoomIn, ZoomOut } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

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

const controlButton =
  "border-0 bg-card text-muted-foreground hover:bg-accent hover:text-foreground";

// React Flow's own control CSS loads after Tailwind and fills its icons at
// 12px, which turns a stroked Lucide glyph into a solid blob. Inline wins
// without a specificity fight.
const controlIcon = { fill: "none", width: 16, height: 16, maxWidth: "none", maxHeight: "none" };

/** Zoom, reset and full screen, over whichever graph encloses it. */
export function GraphControls() {
  const { fitView, zoomIn, zoomOut } = useReactFlow();
  const canvas = useStore((state) => state.domNode);
  const [fullscreen, setFullscreen] = useState(false);

  useEffect(() => {
    const onChange = () => {
      setFullscreen(document.fullscreenElement === canvas);
      void fitView(glide());
    };

    document.addEventListener("fullscreenchange", onChange);

    return () => document.removeEventListener("fullscreenchange", onChange);
  }, [fitView, canvas]);

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
      style={{ boxShadow: "none" }}
    >
      <ControlButton
        onClick={() => zoomIn(glide())}
        title="Zoom in"
        aria-label="Zoom in"
        className={controlButton}
      >
        <ZoomIn style={controlIcon} />
      </ControlButton>

      <ControlButton
        onClick={() => zoomOut(glide())}
        title="Zoom out"
        aria-label="Zoom out"
        className={controlButton}
      >
        <ZoomOut style={controlIcon} />
      </ControlButton>

      <ControlButton
        onClick={() => {
          void fitView(glide());
        }}
        title="Reset view"
        aria-label="Reset view"
        className={controlButton}
      >
        <Undo2 style={controlIcon} />
      </ControlButton>

      <ControlButton
        onClick={toggleFullscreen}
        title={fullscreen ? "Exit full screen" : "Full screen"}
        aria-label={fullscreen ? "Exit full screen" : "Full screen"}
        className={controlButton}
      >
        {fullscreen ? <Minimize style={controlIcon} /> : <Fullscreen style={controlIcon} />}
      </ControlButton>
    </Controls>
  );
}
