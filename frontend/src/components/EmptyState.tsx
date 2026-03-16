import { Inbox } from "lucide-react";
import type React from "react";

interface Props {
  message?: string;
  icon?: React.ReactNode;
}

export default function EmptyState({ message = "No results found", icon }: Props) {
  return (
    <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
      {icon || <Inbox className="mb-3 size-10 opacity-40" />}
      <p className="text-sm">{message}</p>
    </div>
  );
}
