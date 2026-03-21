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
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { stackNamespaceLabel } from "@/lib/parseStackLabels";
import type { LucideIcon } from "lucide-react";
import { RefreshCw, RotateCcw, Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";

function ConfirmAction({
  icon: Icon,
  label,
  title,
  description,
  disabled,
  disabledTitle,
  loading,
  error,
  variant = "default",
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
  variant?: "default" | "destructive";
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
            <AlertDialogAction
              variant={variant}
              onClick={onConfirm}
            >
              {label}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {error && <p className="text-xs text-red-600 dark:text-red-400">{error}</p>}
    </div>
  );
}

export function ServiceActions({ service, serviceId }: { service: Service; serviceId: string }) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canWrite = !levelLoading && level >= opsLevel.operational;
  const canImpact = !levelLoading && level >= opsLevel.impactful;

  const navigate = useNavigate();
  const rollback = useAsyncAction();
  const restart = useAsyncAction();
  const remove = useAsyncAction();

  if (!canWrite && !canImpact) {
    return null;
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {canWrite && (
        <ConfirmAction
          icon={RotateCcw}
          label="Rollback"
          title="Rollback service?"
          description="This will rollback the service to its previous specification."
          disabled={!service.PreviousSpec}
          disabledTitle="No previous spec available"
          loading={rollback.loading}
          error={rollback.error}
          onConfirm={() =>
            void rollback.execute(() => api.rollbackService(serviceId), "Failed to rollback")
          }
        />
      )}

      {canWrite && (
        <ConfirmAction
          icon={RefreshCw}
          label="Restart"
          title="Restart service?"
          description="This triggers a rolling restart of all tasks."
          loading={restart.loading}
          error={restart.error}
          onConfirm={() =>
            void restart.execute(() => api.restartService(serviceId), "Failed to restart")
          }
        />
      )}

      {canImpact && (
        <ConfirmAction
          icon={Trash2}
          label="Remove"
          title="Remove service?"
          description="This will permanently remove the service and all its tasks. This action cannot be undone."
          loading={remove.loading}
          error={remove.error}
          variant="destructive"
          onConfirm={() =>
            void remove.execute(async () => {
              await api.removeService(serviceId);
              const stackName = service.Spec?.Labels?.[stackNamespaceLabel];
              navigate(stackName ? `/stacks/${stackName}` : "/services", { replace: true });
            }, "Failed to remove service")
          }
        />
      )}
    </div>
  );
}
