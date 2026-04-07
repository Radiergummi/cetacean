import { ChevronRight } from "lucide-react";
import type React from "react";
import { useCallback, useState } from "react";
import { useLocation } from "react-router-dom";

function sectionKey(scope: string, title: string) {
  const normalizedTitle = title.toLowerCase().replace(/\s+/g, "-");

  return `section:${scope}:${normalizedTitle}`;
}

/**
 * Derives a stable page scope from the current pathname.
 * Strips trailing ID/name segments so all detail pages of the same type
 * share collapse state (e.g., /services/abc and /services/def both → "services").
 */
function usePageScope(): string {
  const { pathname } = useLocation();
  const segments = pathname.split("/").filter(Boolean);

  if (segments.length >= 2) {
    return segments[0];
  }

  return segments[0] ?? "root";
}

/**
 * localStorage-backed open/closed state for a named section.
 * Keys are scoped by page type to prevent collisions across same-named
 * sections on different pages (e.g. "Labels" on service vs. node detail).
 */
export function useSectionCollapse(title: string, defaultOpen = true) {
  const scope = usePageScope();

  const [open, setOpen] = useState(() => {
    try {
      const stored = localStorage.getItem(sectionKey(scope, title));

      return stored !== null ? stored === "1" : defaultOpen;
    } catch {
      return defaultOpen;
    }
  });

  const toggle = useCallback(() => {
    setOpen((prev) => {
      try {
        localStorage.setItem(sectionKey(scope, title), prev ? "0" : "1");
      } catch {
        /* ignore */
      }

      return !prev;
    });
  }, [scope, title]);

  return { open, toggle } as const;
}

/**
 * Chevron toggle button used by CollapsibleSection and custom widget headers.
 */
export function SectionToggle({
  title,
  open,
  onToggle,
  className,
}: {
  title: React.ReactNode;
  open: boolean;
  onToggle: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={open}
      className={
        className ??
        "flex cursor-pointer items-center gap-1.5 rounded text-sm font-medium tracking-wider text-muted-foreground uppercase " +
          "transition-colors outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
      }
    >
      <ChevronRight
        data-open={open || undefined}
        className="h-4 w-4 transition-transform data-open:rotate-90"
      />
      {title}
    </button>
  );
}

export default function CollapsibleSection({
  title,
  children,
  controls,
  defaultOpen = true,
}: {
  title: string;
  children: React.ReactNode;
  controls?: React.ReactNode;
  defaultOpen?: boolean;
}) {
  const { open, toggle } = useSectionCollapse(title, defaultOpen);

  return (
    <div>
      <div className="mb-3 flex min-h-8 flex-wrap items-center gap-2">
        <SectionToggle
          title={title}
          open={open}
          onToggle={toggle}
        />
        {open && controls && <div className="flex items-center gap-2 sm:ms-auto">{controls}</div>}
      </div>
      {open && children}
    </div>
  );
}
