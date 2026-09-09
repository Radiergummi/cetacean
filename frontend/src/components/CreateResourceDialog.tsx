import { useAsyncAction } from "../hooks/useAsyncAction";
import { Button } from "./ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "./ui/dialog";
import { Plus } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useNavigate } from "react-router-dom";

interface CreateResourceDialogProps {
  resourceType: string;
  onSubmit: () => Promise<string>;
  children: ReactNode;
  canSubmit: boolean;
  onReset: () => void;
  canCreate?: boolean | undefined;
}

export default function CreateResourceDialog({
  resourceType,
  onSubmit,
  children,
  canSubmit,
  onReset,
  canCreate = false,
}: CreateResourceDialogProps) {
  const [open, setOpen] = useState(false);
  const action = useAsyncAction({ toast: true });
  const navigate = useNavigate();

  function handleOpenChange(next: boolean) {
    if (!next) {
      onReset();
    }

    setOpen(next);
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
    >
      <DialogTrigger
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

      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>Create {resourceType}</DialogTitle>
          <DialogDescription>
            Create a new {resourceType.toLowerCase()} in the swarm.
          </DialogDescription>
        </DialogHeader>

        {/*
          A real form, so Enter submits from any field and the browser runs its
          own validation first. The fields and the Create button were previously
          only adjacent, which left Enter doing nothing at all.
        */}
        <form
          onSubmit={(event) => {
            event.preventDefault();

            if (!canSubmit || action.loading) {
              return;
            }

            void action.execute(async () => {
              const path = await onSubmit();
              setOpen(false);
              onReset();
              navigate(path);
            }, `Failed to create ${resourceType.toLowerCase()}`);
          }}
        >
          {children}

          <DialogFooter>
            <DialogClose
              render={
                <Button
                  variant="outline"
                  type="button"
                />
              }
            >
              Cancel
            </DialogClose>
            <Button
              type="submit"
              disabled={!canSubmit || action.loading}
            >
              {action.loading ? "Creating\u2026" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
