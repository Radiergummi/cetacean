import { ChevronRight } from "lucide-react";
import type React from "react";
import { useCallback, useState } from "react";

function sectionKey(title: string) {
  return `section:${title.toLowerCase().replace(/\s+/g, "-")}`;
}

/** localStorage-backed open/closed state for a named section. */
export function useSectionCollapse(title: string, defaultOpen = true) {
  const [open, setOpen] = useState(() => {
    try {
      const stored = localStorage.getItem(sectionKey(title));
      return stored !== null ? stored === "1" : defaultOpen;
    } catch {
      return defaultOpen;
    }
  });
  const toggle = useCallback(() => {
    setOpen((prev) => {
      try {
        localStorage.setItem(sectionKey(title), prev ? "0" : "1");
      } catch {
        /* ignore */
      }
      return !prev;
    });
  }, [title]);

  return { open, toggle } as const;
}

/** Chevron toggle button used by CollapsibleSection and custom widget headers. */
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
      className={
        className ??
        "flex cursor-pointer items-center gap-1.5 text-sm font-medium tracking-wider text-muted-foreground uppercase transition-colors hover:text-foreground"
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
      <div className="mb-3 flex min-h-8 items-center gap-2">
        <SectionToggle
          title={title}
          open={open}
          onToggle={toggle}
        />
        {open && controls && <div className="ml-auto flex items-center gap-2">{controls}</div>}
      </div>
      {open && children}
    </div>
  );
}
