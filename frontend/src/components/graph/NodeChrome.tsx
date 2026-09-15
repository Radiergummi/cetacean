import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useCoarsePointer } from "@/hooks/useMediaQuery";
import { cn } from "@/lib/utils";
import { Handle, Position } from "@xyflow/react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";

const focusRing = "focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none";

/**
 * Every node carries both handles so an edge always finds an anchor; the one
 * a given column does not use is invisible rather than absent.
 */
export function Ports() {
  return (
    <>
      <Handle
        type="target"
        position={Position.Left}
        className="opacity-0"
        isConnectable={false}
      />
      <Handle
        type="source"
        position={Position.Right}
        className="opacity-0"
        isConnectable={false}
      />
    </>
  );
}

export function DetailList({ children }: { children: ReactNode }) {
  return <dl className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">{children}</dl>;
}

export function Detail({ term, children }: { term: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{term}</dt>
      <dd className="font-mono break-all">{children}</dd>
    </>
  );
}

/**
 * A node's face and the detail behind it. A pointer opens the detail by
 * hovering and a keyboard by focusing; a touch screen does neither, so there
 * the face becomes a popover a tap opens, and the link it would have been
 * moves inside — a tap cannot both reveal and navigate.
 */
export function NodeDetail({
  href,
  label,
  className,
  face,
  children,
}: {
  href?: string | undefined;
  label: string;
  className: string;
  face: ReactNode;
  children: ReactNode;
}) {
  const coarse = useCoarsePointer();

  const body = (
    <>
      <Ports />
      {face}
    </>
  );

  if (coarse) {
    return (
      <Popover>
        <PopoverTrigger
          render={
            <button
              type="button"
              aria-label={label}
              className={cn(className, focusRing)}
            >
              {body}
            </button>
          }
        />
        <PopoverContent className="w-auto max-w-72 gap-1.5 text-xs">
          <div>{children}</div>

          {href && (
            <Link
              to={href}
              className="font-medium text-link hover:underline"
            >
              Open
            </Link>
          )}
        </PopoverContent>
      </Popover>
    );
  }

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          href ? (
            <Link
              to={href}
              aria-label={label}
              className={cn(className, focusRing)}
            >
              {body}
            </Link>
          ) : (
            <button
              type="button"
              aria-label={label}
              className={cn(className, focusRing)}
            >
              {body}
            </button>
          )
        }
      />
      <TooltipContent>{children}</TooltipContent>
    </Tooltip>
  );
}
