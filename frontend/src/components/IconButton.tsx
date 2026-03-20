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
  active?: boolean;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
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
