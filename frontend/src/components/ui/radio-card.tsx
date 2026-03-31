import { cn } from "@/lib/utils";
import type { ReactNode } from "react";
import { useCallback, useRef } from "react";

interface RadioCardGroupProps {
  children: ReactNode;
  className?: string;
}

/**
 * Wraps a set of RadioCard buttons with roving tabindex and arrow-key navigation.
 * Only the selected (or first) card is tabbable; arrow keys move focus and activate.
 */
export function RadioCardGroup({ children, className }: RadioCardGroupProps) {
  const groupRef = useRef<HTMLDivElement>(null);

  const handleKeyDown = useCallback((event: React.KeyboardEvent<HTMLDivElement>) => {
    const group = groupRef.current;

    if (!group) {
      return;
    }

    const cards = Array.from(
      group.querySelectorAll<HTMLButtonElement>('[role="radio"]:not(:disabled)'),
    );

    if (cards.length === 0) {
      return;
    }

    const currentIndex = cards.indexOf(document.activeElement as HTMLButtonElement);

    if (currentIndex === -1) {
      return;
    }

    let nextIndex: number | null = null;

    if (event.key === "ArrowRight" || event.key === "ArrowDown") {
      nextIndex = (currentIndex + 1) % cards.length;
    } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
      nextIndex = (currentIndex - 1 + cards.length) % cards.length;
    }

    if (nextIndex !== null) {
      event.preventDefault();
      cards[nextIndex].focus();
      cards[nextIndex].click();
    }
  }, []);

  return (
    <div
      ref={groupRef}
      role="radiogroup"
      onKeyDown={handleKeyDown}
      className={className}
    >
      {children}
    </div>
  );
}

interface RadioCardProps {
  selected: boolean;
  onClick: () => void;
  icon?: ReactNode;
  title: string;
  description: string;
  disabled?: boolean;
}

export function RadioCard({
  selected,
  onClick,
  icon,
  title,
  description,
  disabled,
}: RadioCardProps) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      tabIndex={selected ? 0 : -1}
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
