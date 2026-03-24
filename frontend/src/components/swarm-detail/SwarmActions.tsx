import { api } from "@/api/client";
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
import type { LucideIcon } from "lucide-react";
import { RefreshCw } from "lucide-react";

function ConfirmAction({
  icon: Icon,
  label,
  title,
  description,
  loading,
  onConfirm,
}: {
  icon: LucideIcon;
  label: string;
  title: string;
  description: string;
  loading: boolean;
  onConfirm: () => void;
}) {
  return (
    <div className="flex flex-col items-start gap-1">
      <AlertDialog>
        <AlertDialogTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              disabled={loading}
            >
              {loading ? <Spinner className="size-3" /> : <Icon className="size-3.5" />}
              {label}
            </Button>
          }
        />
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
    </div>
  );
}

export function SwarmActions({ onRotated }: { onRotated: () => void }) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canImpact = !levelLoading && level >= opsLevel.impactful;

  const rotateWorker = useAsyncAction({ toast: true });
  const rotateManager = useAsyncAction({ toast: true });

  if (!canImpact) {
    return null;
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <ConfirmAction
        icon={RefreshCw}
        label="Rotate Worker Token"
        title="Rotate worker join token?"
        description="This will invalidate the current worker join token. Existing workers are not affected."
        loading={rotateWorker.loading}
        onConfirm={() =>
          void rotateWorker.execute(async () => {
            await api.rotateToken("worker");
            onRotated();
          }, "Failed to rotate worker token")
        }
      />

      <ConfirmAction
        icon={RefreshCw}
        label="Rotate Manager Token"
        title="Rotate manager join token?"
        description="This will invalidate the current manager join token. Existing managers are not affected."
        loading={rotateManager.loading}
        onConfirm={() =>
          void rotateManager.execute(async () => {
            await api.rotateToken("manager");
            onRotated();
          }, "Failed to rotate manager token")
        }
      />
    </div>
  );
}
