import { cn } from "@/lib/utils";
import type { ReactNode } from "react";

interface RadioCardProps {
  selected: boolean;
  onClick: () => void;
  icon?: ReactNode;
  title: string;
  description: string;
  disabled?: boolean;
}

export function RadioCard({ selected, onClick, icon, title, description, disabled }: RadioCardProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "flex items-start gap-3 rounded-lg border p-3 text-left outline-none transition-colors focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
        selected
          ? "border-primary bg-primary/5 ring-1 ring-primary"
          : "border-border hover:border-muted-foreground/40",
        disabled && "pointer-events-none opacity-50",
      )}
    >
      {icon && <div className="mt-0.5 shrink-0 text-muted-foreground">{icon}</div>}

      <div className="flex-1">
        <div className="text-sm font-medium">{title}</div>
        <div className="text-xs text-muted-foreground">{description}</div>
      </div>

      <div
        className={cn(
          "mt-0.5 size-4 shrink-0 rounded-full border-2 transition-colors",
          selected ? "border-primary bg-primary" : "border-muted-foreground/40",
        )}
      >
        {selected && (
          <div className="flex size-full items-center justify-center">
            <div className="size-1.5 rounded-full bg-primary-foreground" />
          </div>
        )}
      </div>
    </button>
  );
}
