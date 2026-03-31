import { api } from "@/api/client";
import { DockerDocsLink } from "@/components/service-detail/DockerDocsLink";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { RadioCard, RadioCardGroup } from "@/components/ui/radio-card";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { Globe, Pencil, Shuffle } from "lucide-react";
import { useState } from "react";

type EndpointMode = "vip" | "dnsrr";

export function EndpointModeEditor({
  serviceId,
  currentMode,
}: {
  serviceId: string;
  currentMode: EndpointMode;
}) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.impactful;

  const [editing, setEditing] = useState(false);
  const [mode, setMode] = useState<EndpointMode>(currentMode);
  useEscapeCancel(editing, () => setEditing(false));
  const action = useAsyncAction({ toast: true });

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
        <h3 className="flex items-center gap-1 text-xs font-medium tracking-wider text-muted-foreground uppercase">
          Endpoint Mode
          {editing && (
            <DockerDocsLink href="https://docs.docker.com/reference/compose-file/services/#endpoint_mode" />
          )}
        </h3>
        {!editing && canEdit && (
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
          <RadioCardGroup className="flex flex-col gap-2">
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
          </RadioCardGroup>

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
