import { api, ApiError } from "@/api/client";
import type { Node } from "@/api/types";
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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { opsLevel, useOperationsLevel } from "@/hooks/useOperationsLevel";
import { getErrorInfo } from "@/lib/errors";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

export function NodeActions({ node }: { node: Node }) {
  const { level, loading: levelLoading } = useOperationsLevel();
  const canImpact = !levelLoading && level >= opsLevel.impactful;
  const navigate = useNavigate();
  const remove = useAsyncAction();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [confirmText, setConfirmText] = useState("");

  if (!canImpact) {
    return null;
  }

  const hostname = node.Description?.Hostname || node.ID;
  const isDown = node.Status?.State === "down";
  const canRemove = isDown && confirmText === hostname;

  const errorCode = remove.cause instanceof ApiError ? remove.cause.code : null;
  const errorInfo = getErrorInfo(errorCode);
  const showForceRemove = errorInfo?.action === "force-remove";

  function handleOpenChange(next: boolean) {
    setDialogOpen(next);

    if (!next) {
      setConfirmText("");
    }
  }

  const trigger = (
    <Button
      variant="outline"
      size="sm"
      disabled={!isDown || remove.loading}
      className={
        isDown
          ? "border-red-500/50 text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/20"
          : ""
      }
      onClick={() => setDialogOpen(true)}
    >
      {remove.loading ? <Spinner className="size-3" /> : <Trash2 className="size-3.5" />}
      Remove
    </Button>
  );

  return (
    <div className="flex flex-col items-start gap-1">
      {!isDown ? (
        <Tooltip>
          <TooltipTrigger render={trigger} />
          <TooltipContent>Node must be in down state to remove</TooltipContent>
        </Tooltip>
      ) : (
        trigger
      )}

      {remove.error && (
        <div className="flex items-center gap-2">
          <p className="text-xs text-red-600 dark:text-red-400">
            {errorInfo?.suggestion ?? remove.error}
          </p>
          {showForceRemove && (
            <Button
              variant="destructive"
              size="sm"
              disabled={remove.loading}
              onClick={() =>
                void remove.execute(async () => {
                  await api.removeNode(node.ID, true);
                  navigate("/nodes", { replace: true });
                }, "Failed to force remove node")
              }
            >
              Force remove
            </Button>
          )}
        </div>
      )}

      <AlertDialog
        open={dialogOpen}
        onOpenChange={handleOpenChange}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove node?</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently remove <strong className="text-foreground">{hostname}</strong>{" "}
              from the swarm. The node will no longer appear in the cluster and cannot rejoin
              without being re-initialized. Any node-specific labels and configuration will be lost.
            </AlertDialogDescription>
          </AlertDialogHeader>

          <div className="flex flex-col gap-1.5">
            <label className="text-sm text-foreground">
              Type <strong className="text-foreground">{hostname}</strong> to confirm
            </label>
            <Input
              value={confirmText}
              onChange={(event) => setConfirmText(event.target.value)}
              placeholder={hostname}
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
            />
          </div>

          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={!canRemove || remove.loading}
              onClick={() =>
                void remove.execute(async () => {
                  await api.removeNode(node.ID);
                  navigate("/nodes", { replace: true });
                }, "Failed to remove node")
              }
            >
              {remove.loading ? "Removing…" : "Remove"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
