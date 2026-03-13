import type React from "react";

export function IconButton({
  onClick,
  title,
  icon,
  active,
}: {
  onClick: () => void;
  title: string;
  icon: React.ReactNode;
  active?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-pressed={active || undefined}
      className="size-8 flex items-center justify-center rounded-md border border-border bg-background hover:bg-muted
      aria-pressed:bg-primary aria-pressed:text-primary-foreground aria-pressed:border-primary cursor-pointer"
    >
      {icon}
    </button>
  );
}
