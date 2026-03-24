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
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

interface StackActionsProps {
  stackName: string;
  resourceCounts: {
    services: number;
    networks: number;
    configs: number;
    secrets: number;
  };
}

export function StackActions({ stackName, resourceCounts }: StackActionsProps) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canImpact = !levelLoading && level >= opsLevel.impactful;
  const navigate = useNavigate();
  const remove = useAsyncAction({ toast: true });
  const [dialogOpen, setDialogOpen] = useState(false);
  const [confirmText, setConfirmText] = useState("");
  const [partialErrors, setPartialErrors] = useState<
    { type: string; id: string; error: string }[] | null
  >(null);

  if (!canImpact) {
    return null;
  }

  const canRemove = confirmText === stackName;

  function handleOpenChange(next: boolean) {
    setDialogOpen(next);

    if (!next) {
      setConfirmText("");
      setPartialErrors(null);
    }
  }

  const total =
    resourceCounts.services +
    resourceCounts.networks +
    resourceCounts.configs +
    resourceCounts.secrets;

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className="border-red-500/50 text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/20"
        disabled={remove.loading}
        onClick={() => setDialogOpen(true)}
      >
        {remove.loading ? <Spinner className="size-3" /> : <Trash2 className="size-3.5" />}
        Remove
      </Button>

      <AlertDialog
        open={dialogOpen}
        onOpenChange={handleOpenChange}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove stack?</AlertDialogTitle>
            <AlertDialogDescription>
              This will remove all services, networks, configs, and secrets in the{" "}
              <strong className="text-foreground">{stackName}</strong> stack. Volumes will not be
              removed.
            </AlertDialogDescription>
          </AlertDialogHeader>

          {total > 0 && (
            <p className="text-sm text-muted-foreground">
              This stack contains {resourceCounts.services} services, {resourceCounts.networks}{" "}
              networks, {resourceCounts.configs} configs, {resourceCounts.secrets} secrets.
            </p>
          )}

          <div className="flex flex-col gap-1.5">
            <label className="text-sm text-foreground">
              Type <strong className="text-foreground">{stackName}</strong> to confirm
            </label>
            <Input
              value={confirmText}
              onChange={(event) => setConfirmText(event.target.value)}
              placeholder={stackName}
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
            />
          </div>

          {partialErrors && (
            <div className="rounded-md border border-yellow-500/25 bg-yellow-500/5 px-3 py-2 text-xs leading-relaxed text-yellow-600 dark:text-yellow-500">
              <p className="mb-1 font-medium">Some resources could not be removed:</p>
              <ul className="list-inside list-disc">
                {partialErrors.map(({ type, id, error }, index) => (
                  <li key={index}>
                    {type} {id}: {error}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={!canRemove || remove.loading}
              onClick={() =>
                void remove.execute(async () => {
                  const result = await api.removeStack(stackName);

                  if (result.errors && result.errors.length > 0) {
                    setPartialErrors(result.errors);
                    return;
                  }

                  navigate("/stacks", { replace: true });
                }, "Failed to remove stack")
              }
            >
              {remove.loading ? "Removing…" : "Remove"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
