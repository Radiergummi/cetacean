import { api } from "@/api/client";
import InfoCard from "@/components/InfoCard";
import { Spinner } from "@/components/Spinner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { cn } from "@/lib/utils";
import { CircleCheck, CirclePause, LogOut, Pencil } from "lucide-react";
import { useState } from "react";

type Availability = "active" | "pause" | "drain";

const availabilityOptions = [
  {
    value: "active" as const,
    icon: CircleCheck,
    title: "Active",
    description: "Accepts new tasks from the scheduler.",
  },
  {
    value: "pause" as const,
    icon: CirclePause,
    title: "Pause",
    description: "Keeps running tasks but won't receive new ones.",
  },
  {
    value: "drain" as const,
    icon: LogOut,
    title: "Drain",
    description: "All tasks are rescheduled to other nodes.",
  },
];

export function AvailabilityEditor({ nodeId, current }: { nodeId: string; current: string }) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canEdit = !levelLoading && level >= opsLevel.impactful;
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState<Availability>(current as Availability);
  const [drainPending, setDrainPending] = useState(false);
  const action = useAsyncAction();

  function handleOpenChange(nextOpen: boolean) {
    if (nextOpen) {
      setValue(current as Availability);
    }

    setOpen(nextOpen);
  }

  async function save() {
    if (value === current) {
      setOpen(false);
      return;
    }

    if (value === "drain" && current !== "drain") {
      setDrainPending(true);
      return;
    }

    await action.execute(async () => {
      await api.updateNodeAvailability(nodeId, value);
      setOpen(false);
    }, "Failed to update availability");
  }

  async function confirmDrain() {
    setDrainPending(false);
    await action.execute(async () => {
      await api.updateNodeAvailability(nodeId, "drain");
      setOpen(false);
    }, "Failed to update availability");
  }

  const cardValue = (
    <>
      {availabilityOptions.find(({ value: v }) => v === current)?.title ?? current}
      {canEdit && <Popover
        open={open}
        onOpenChange={handleOpenChange}
        modal
      >
        <PopoverTrigger
          render={
            <Button
              variant="ghost"
              size="icon-xs"
              title="Edit availability"
            >
              <Pencil className="size-3.5" />
            </Button>
          }
        />
        <PopoverContent
          className="w-72"
          align="end"
        >
          <div className="mb-3 flex flex-col gap-2">
            {availabilityOptions.map((option) => (
              <button
                key={option.value}
                type="button"
                onClick={() => setValue(option.value)}
                disabled={action.loading}
                className={cn(
                  "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors",
                  value === option.value
                    ? "border-primary bg-primary/5 ring-1 ring-primary"
                    : "border-border hover:border-muted-foreground/40",
                  action.loading && "pointer-events-none opacity-50",
                )}
              >
                <option.icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />

                <div className="flex-1">
                  <div className="text-sm font-medium">{option.title}</div>
                  <div className="text-xs text-muted-foreground">{option.description}</div>
                </div>

                <div
                  className={cn(
                    "mt-0.5 size-4 shrink-0 rounded-full border-2 transition-colors",
                    value === option.value
                      ? "border-primary bg-primary"
                      : "border-muted-foreground/40",
                  )}
                >
                  {value === option.value && (
                    <div className="flex size-full items-center justify-center">
                      <div className="size-1.5 rounded-full bg-primary-foreground" />
                    </div>
                  )}
                </div>
              </button>
            ))}
          </div>

          {action.error && (
            <p className="mb-2 text-xs text-red-600 dark:text-red-400">{action.error}</p>
          )}

          <div className="flex gap-2">
            <Button
              size="sm"
              className="flex-1"
              onClick={() => void save()}
              disabled={action.loading}
            >
              {action.loading && <Spinner className="size-3" />}
              Apply
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="flex-1"
              onClick={() => handleOpenChange(false)}
              disabled={action.loading}
            >
              Cancel
            </Button>
          </div>
        </PopoverContent>
      </Popover>}
    </>
  );

  return (
    <>
      <InfoCard
        label="Availability"
        value={cardValue}
      />
      <AlertDialog
        open={drainPending}
        onOpenChange={setDrainPending}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Drain this node?</AlertDialogTitle>
            <AlertDialogDescription>
              Draining this node will reschedule all running tasks to other nodes.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirmDrain()}>Drain</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
