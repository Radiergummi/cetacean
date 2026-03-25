import { type ReactNode, useState } from "react";
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
} from "./ui/alert-dialog";
import { Button } from "./ui/button";
import { Plus } from "lucide-react";
import { opsLevel, useOperationsLevel } from "../hooks/useOperationsLevel";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useNavigate } from "react-router-dom";

interface CreateResourceDialogProps {
  resourceType: string;
  onSubmit: () => Promise<string>;
  children: ReactNode;
  canSubmit: boolean;
  onReset: () => void;
}

export default function CreateResourceDialog({
  resourceType,
  onSubmit,
  children,
  canSubmit,
  onReset,
}: CreateResourceDialogProps) {
  const [open, setOpen] = useState(false);
  const { level, loading: levelLoading } = useOperationsLevel();
  const canCreate = !levelLoading && level >= opsLevel.configuration;
  const action = useAsyncAction({ toast: true });
  const navigate = useNavigate();

  function handleOpenChange(next: boolean) {
    if (!next) {
      onReset();
    }

    setOpen(next);
  }

  return (
    <AlertDialog
      open={open}
      onOpenChange={handleOpenChange}
    >
      <AlertDialogTrigger
        render={
          <Button
            size="sm"
            disabled={!canCreate}
            title={canCreate ? `Create ${resourceType.toLowerCase()}` : "Operations level too low"}
          >
            <Plus className="size-4" />
            Create
          </Button>
        }
      />

      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Create {resourceType}</AlertDialogTitle>
          <AlertDialogDescription>
            Create a new {resourceType.toLowerCase()} in the swarm.
          </AlertDialogDescription>
        </AlertDialogHeader>

        {children}

        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            disabled={!canSubmit || action.loading}
            onClick={() => {
              void action.execute(async () => {
                const path = await onSubmit();
                setOpen(false);
                onReset();
                navigate(path);
              }, `Failed to create ${resourceType.toLowerCase()}`);
            }}
          >
            {action.loading ? "Creating\u2026" : "Create"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
