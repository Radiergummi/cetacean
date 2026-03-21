import { api } from "@/api/client";
import InfoCard from "@/components/InfoCard";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { RadioCard } from "@/components/ui/radio-card";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { cn } from "@/lib/utils";
import { Pencil } from "lucide-react";
import { useState } from "react";

interface RoleEditorProps {
  nodeId: string;
  currentRole: "worker" | "manager";
  isLeader: boolean;
  managerCount: number | null;
}

const roles = [
  {
    value: "worker",
    title: "Worker",
    description: "Runs tasks. Cannot participate in Raft consensus or manage the cluster.",
  },
  {
    value: "manager",
    title: "Manager",
    description:
      "Participates in Raft consensus. Can manage nodes, services, and other cluster resources.",
  },
] as const;

export function RoleEditor({ nodeId, currentRole, isLeader, managerCount }: RoleEditorProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.impactful;
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState(currentRole);
  const action = useAsyncAction();

  function handleOpenChange(next: boolean) {
    if (next) {
      setValue(currentRole);
    }

    setOpen(next);
  }

  async function save() {
    if (value === currentRole) {
      setOpen(false);
      return;
    }

    await action.execute(async () => {
      await api.updateNodeRole(nodeId, value);
      setOpen(false);
    }, "Failed to update role");
  }

  const isDemoting = currentRole === "manager" && value === "worker";
  const quorum = managerCount !== null ? Math.floor(managerCount / 2) + 1 : null;
  const remainingManagers = managerCount !== null ? managerCount - 1 : null;

  return (
    <InfoCard
      label="Role"
      value={
        <>
          <span className="capitalize">{currentRole}</span>
          {currentRole === "manager" && isLeader && (
            <span className="rounded bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
              Leader
            </span>
          )}
          {canEdit && (
            <Popover
              open={open}
              onOpenChange={handleOpenChange}
              modal
            >
              <PopoverTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    title="Edit role"
                  >
                    <Pencil className="size-3.5" />
                  </Button>
                }
              />

              <PopoverContent className="w-80">
                <div className="flex flex-col gap-3">
                  <p className="text-sm font-medium">Change Role</p>

                  {roles.map((role) => (
                    <RadioCard
                      key={role.value}
                      selected={value === role.value}
                      onClick={() => setValue(role.value)}
                      disabled={role.value === currentRole}
                      title={role.value === currentRole ? `${role.title} (current)` : role.title}
                      description={role.description}
                    />
                  ))}

                  {isDemoting && (
                    <div className="rounded-md border border-yellow-500/25 bg-yellow-500/5 px-3 py-2 text-xs leading-relaxed text-yellow-600 dark:text-yellow-500">
                      {isLeader && (
                        <p className={cn("font-medium", quorum !== null && "mb-2")}>
                          This node is the Raft leader. Demoting it will trigger a leader
                          re-election.
                        </p>
                      )}
                      {quorum !== null && remainingManagers !== null && (
                        <p>
                          This cluster has {managerCount} managers. Demoting this node leaves{" "}
                          {remainingManagers} managers (quorum requires {quorum}).
                          {remainingManagers === quorum &&
                            " Losing one more manager will make the cluster unrecoverable."}
                        </p>
                      )}
                    </div>
                  )}

                  {action.error && (
                    <p className="text-xs text-red-600 dark:text-red-400">{action.error}</p>
                  )}

                  <div className="flex justify-end gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setOpen(false)}
                    >
                      Cancel
                    </Button>
                    <Button
                      size="sm"
                      disabled={value === currentRole || action.loading}
                      onClick={() => void save()}
                    >
                      {action.loading ? "Applying…" : "Apply"}
                    </Button>
                  </div>
                </div>
              </PopoverContent>
            </Popover>
          )}
        </>
      }
    />
  );
}
