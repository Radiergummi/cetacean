import { api } from "@/api/client";
import type { Service } from "@/api/types";
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
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { useOperationsLevel } from "@/hooks/useOperationsLevel";
import type { LucideIcon } from "lucide-react";
import { RefreshCw, RotateCcw } from "lucide-react";

function ConfirmAction({
  icon: Icon,
  label,
  title,
  description,
  disabled,
  disabledTitle,
  loading,
  error,
  onConfirm,
}: {
  icon: LucideIcon;
  label: string;
  title: string;
  description: string;
  disabled?: boolean;
  disabledTitle?: string;
  loading: boolean;
  error: string | null;
  onConfirm: () => void;
}) {
  const trigger = (
    <AlertDialogTrigger
      render={
        <Button
          variant="outline"
          size="sm"
          disabled={disabled || loading}
        >
          {loading ? <Spinner className="size-3" /> : <Icon className="size-3.5" />}
          {label}
        </Button>
      }
    />
  );

  return (
    <div className="flex flex-col items-start gap-1">
      <AlertDialog>
        {disabled && disabledTitle ? (
          <Tooltip>
            <TooltipTrigger render={trigger} />
            <TooltipContent>{disabledTitle}</TooltipContent>
          </Tooltip>
        ) : (
          trigger
        )}
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{title}</AlertDialogTitle>
            <AlertDialogDescription>{description}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={onConfirm}>{label}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {error && <p className="text-xs text-red-600 dark:text-red-400">{error}</p>}
    </div>
  );
}

export function ServiceActions({ service, serviceId }: { service: Service; serviceId: string }) {
  const { level } = useOperationsLevel();
  const canWrite = level >= 1;

  const rollback = useAsyncAction();
  const restart = useAsyncAction();

  const canRollback = canWrite && !!service.PreviousSpec;

  return (
    <div className="flex flex-wrap items-center gap-2">
      <ConfirmAction
        icon={RotateCcw}
        label="Rollback"
        title="Rollback service?"
        description="This will rollback the service to its previous specification."
        disabled={!canRollback}
        disabledTitle={
          !canWrite ? "Editing disabled by server configuration" : "No previous spec available"
        }
        loading={rollback.loading}
        error={rollback.error}
        onConfirm={() =>
          void rollback.execute(() => api.rollbackService(serviceId), "Failed to rollback")
        }
      />

      <ConfirmAction
        icon={RefreshCw}
        label="Restart"
        title="Restart service?"
        description="This triggers a rolling restart of all tasks."
        disabled={!canWrite}
        disabledTitle="Editing disabled by server configuration"
        loading={restart.loading}
        error={restart.error}
        onConfirm={() =>
          void restart.execute(() => api.restartService(serviceId), "Failed to restart")
        }
      />
    </div>
  );
}
