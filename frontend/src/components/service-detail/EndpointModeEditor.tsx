import { api } from "@/api/client";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { cn } from "@/lib/utils";
import { Globe, Pencil, Shuffle } from "lucide-react";
import { useState } from "react";

type EndpointMode = "vip" | "dnsrr";

interface RadioCardProps {
  selected: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  title: string;
  description: string;
  disabled?: boolean;
}

function RadioCard({ selected, onClick, icon, title, description, disabled }: RadioCardProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors",
        selected
          ? "border-primary bg-primary/5 ring-1 ring-primary"
          : "border-border hover:border-muted-foreground/40",
        disabled && "pointer-events-none opacity-50",
      )}
    >
      <div className="mt-0.5 shrink-0 text-muted-foreground">{icon}</div>

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

export function EndpointModeEditor({
  serviceId,
  currentMode,
}: {
  serviceId: string;
  currentMode: EndpointMode;
}) {
  const [editing, setEditing] = useState(false);
  const [mode, setMode] = useState<EndpointMode>(currentMode);
  const action = useAsyncAction();

  function openEdit() {
    setMode(currentMode);
    setEditing(true);
  }

  async function save() {
    if (mode === currentMode) {
      setEditing(false);

      return;
    }

    await action.execute(async () => {
      await api.updateServiceEndpointMode(serviceId, mode);
      setEditing(false);
    }, "Failed to update endpoint mode");
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
          Endpoint Mode
        </h3>
        {!editing && (
          <Button
            variant="outline"
            size="xs"
            onClick={openEdit}
          >
            <Pencil className="size-3" />
            Edit
          </Button>
        )}
      </div>

      {editing ? (
        <>
          <div className="flex flex-col gap-2">
            <RadioCard
              selected={mode === "vip"}
              onClick={() => setMode("vip")}
              icon={<Globe className="size-4" />}
              title="VIP (Virtual IP)"
              description="Each service gets a virtual IP that load-balances across tasks. Supports published ports."
              disabled={action.loading}
            />
            <RadioCard
              selected={mode === "dnsrr"}
              onClick={() => setMode("dnsrr")}
              icon={<Shuffle className="size-4" />}
              title="DNS-RR (DNS Round Robin)"
              description="DNS queries return all task IPs in round-robin order. Does not support published ports."
              disabled={action.loading}
            />
          </div>

          {action.error && <p className="text-xs text-red-600 dark:text-red-400">{action.error}</p>}

          <footer className="flex items-center justify-end gap-2">
            <Button
              size="sm"
              onClick={() => void save()}
              disabled={action.loading}
            >
              {action.loading && <Spinner className="size-3" />}
              Save
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setEditing(false)}
              disabled={action.loading}
            >
              Cancel
            </Button>
          </footer>
        </>
      ) : (
        <div className="flex items-center gap-2 text-sm">
          {currentMode === "vip" ? (
            <>
              <Globe className="size-4 text-muted-foreground" /> VIP (Virtual IP)
            </>
          ) : (
            <>
              <Shuffle className="size-4 text-muted-foreground" /> DNS-RR (DNS Round Robin)
            </>
          )}
        </div>
      )}
    </div>
  );
}
