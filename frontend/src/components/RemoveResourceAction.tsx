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
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { Trash2 } from "lucide-react";
import { useNavigate } from "react-router-dom";

interface RemoveResourceActionProps {
  resourceType: string;
  resourceName: string;
  listPath: string;
  onRemove: () => Promise<void>;
}

export function RemoveResourceAction({
  resourceType,
  resourceName,
  listPath,
  onRemove,
}: RemoveResourceActionProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canImpact = !levelLoading && level >= opsLevel.impactful;
  const navigate = useNavigate();
  const remove = useAsyncAction();

  if (!canImpact) {
    return null;
  }

  return (
    <div className="flex flex-col items-start gap-1">
      <AlertDialog>
        <AlertDialogTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              disabled={remove.loading}
            >
              {remove.loading ? <Spinner className="size-3" /> : <Trash2 className="size-3.5" />}
              Remove
            </Button>
          }
        />
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove {resourceType}?</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently remove{" "}
              <strong className="text-foreground">{resourceName}</strong>. This action cannot be
              undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() =>
                void remove.execute(async () => {
                  await onRemove();
                  navigate(listPath, { replace: true });
                }, `Failed to remove ${resourceType.toLowerCase()}`)
              }
            >
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {remove.error && <p className="text-xs text-red-600 dark:text-red-400">{remove.error}</p>}
    </div>
  );
}
