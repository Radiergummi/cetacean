import { Loader2 } from "lucide-react";

export function Spinner({ className = "size-4" }: { className?: string }) {
  return (
    <Loader2
      className={`animate-spin ${className}`}
      role="status"
      aria-label="Loading"
    />
  );
}
