import { cn } from "@/lib/utils.ts";
import type React from "react";

export function IconButton({
  onClick,
  title,
  icon,
  active,
  className,
}: {
  onClick: () => void;
  title: string;
  icon: React.ReactNode;
  active?: boolean | undefined;
  className?: string | undefined;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      // `title` alone is the last thing the accessible-name algorithm looks
      // at, and several screen readers skip it outright. The icon carries no
      // text, so the name has to be stated.
      aria-label={title}
      aria-pressed={active || undefined}
      className={cn(
        "flex size-8 cursor-pointer items-center justify-center rounded-md border border-border bg-background hover:bg-muted aria-pressed:border-primary aria-pressed:bg-primary aria-pressed:text-primary-foreground",
        className,
      )}
    >
      {icon}
    </button>
  );
}
