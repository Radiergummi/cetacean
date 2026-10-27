import { ApiError } from "@/api/client";
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
import { getErrorInfo } from "@/lib/errors";
import { Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { useNavigate } from "react-router-dom";

interface RemoveResourceActionProps {
  resourceType: string;
  resourceName: string;
  listPath: string;
  onRemove: () => Promise<void>;
  canDelete?: boolean | undefined;
  disabled?: boolean | undefined;
  disabledTitle?: string | undefined;
  /**
   * What removing it costs beyond the resource itself, shown in the confirmation.
   */
  warning?: ReactNode | undefined;
}

export function RemoveResourceAction({
  resourceType,
  resourceName,
  listPath,
  onRemove,
  canDelete = false,
  disabled,
  disabledTitle,
  warning,
}: RemoveResourceActionProps) {
  const navigate = useNavigate();
  const remove = useAsyncAction({ toast: true });

  if (!canDelete) {
    return null;
  }

  const errorCode = remove.cause instanceof ApiError ? remove.cause.code : null;
  const errorInfo = getErrorInfo(errorCode);

  const trigger = (
    <AlertDialogTrigger
      render={
        <Button
          variant="outline"
          size="sm"
          disabled={disabled || remove.loading}
        >
          {remove.loading ? <Spinner className="size-3" /> : <Trash2 className="size-3.5" />}
          Remove
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
            <AlertDialogTitle>Remove {resourceType}?</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently remove{" "}
              <strong className="text-foreground">{resourceName}</strong>. This action cannot be
              undone.
            </AlertDialogDescription>
            {warning && <p className="text-sm text-status-warning">{warning}</p>}
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                void remove.execute(async () => {
                  await onRemove();
                  navigate(listPath, { replace: true });
                }, `Failed to remove ${resourceType.toLowerCase()}`);
              }}
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {remove.error && (
        <p className="text-xs text-status-danger">
          {remove.error}
          {errorInfo && <span className="block text-muted-foreground">{errorInfo.suggestion}</span>}
        </p>
      )}
    </div>
  );
}
