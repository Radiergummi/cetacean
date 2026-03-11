import { Inbox } from "lucide-react";

interface Props {
  message?: string;
  icon?: React.ReactNode;
}

export default function EmptyState({ message = "No results found", icon }: Props) {
  return (
    <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
      {icon || <Inbox className="size-10 mb-3 opacity-40" />}
      <p className="text-sm">{message}</p>
    </div>
  );
}
