import { ChevronRight } from "lucide-react";
import type React from "react";
import { useCallback, useState } from "react";

function sectionKey(title: string) {
  return `section:${title.toLowerCase().replace(/\s+/g, "-")}`;
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
  const [open, setOpen] = useState(() => {
    const stored = localStorage.getItem(sectionKey(title));
    return stored !== null ? stored === "1" : defaultOpen;
  });
  const toggle = useCallback(() => {
    setOpen((prev) => {
      localStorage.setItem(sectionKey(title), prev ? "0" : "1");
      return !prev;
    });
  }, [title]);

  return (
    <div>
      <div className="flex items-center gap-2 mb-3 min-h-8">
        <button
          type="button"
          onClick={toggle}
          className="flex items-center gap-1.5 text-sm font-medium uppercase tracking-wider text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
        >
          <ChevronRight
            data-open={open || undefined}
            className="h-4 w-4 transition-transform data-open:rotate-90"
          />
          {title}
        </button>
        {open && controls && <div className="flex items-center gap-2 ml-auto">{controls}</div>}
      </div>
      {open && children}
    </div>
  );
}
