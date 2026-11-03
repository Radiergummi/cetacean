import AnnotationForm from "./AnnotationForm";
import type { AnnotationScope } from "@/api/types";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useClusterAnnotatable } from "@/hooks/useClusterAnnotatable";
import { MessageSquarePlus } from "lucide-react";
import { useState } from "react";

type ResourceScope = Exclude<AnnotationScope, "cluster">;

/**
 * The form for a new annotation, starting at the moment it mounted.
 */
function NewAnnotationForm({
  scopes,
  pickResource,
  onDone,
}: {
  scopes: AnnotationScope[];
  pickResource: boolean;
  onDone: () => void;
}) {
  const [from] = useState(() => Math.floor(Date.now() / 1000));

  return (
    <AnnotationForm
      scopes={scopes}
      pickResource={pickResource}
      initial={{ from }}
      onDone={onDone}
    />
  );
}

/**
 * The annotation form in a dialog, starting at the moment it opened. With a
 * resource it is about that resource; without one the form searches for one.
 */
export function AddAnnotationDialog({
  open,
  onOpenChange,
  scope,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  scope?: ResourceScope | undefined;
}) {
  const clusterWide = useClusterAnnotatable();
  const cluster: AnnotationScope[] = clusterWide ? ["cluster"] : [];

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add annotation</DialogTitle>
          <DialogDescription>
            An annotation on a moment or a range, for everyone to see.
          </DialogDescription>
        </DialogHeader>

        {open && (
          <NewAnnotationForm
            scopes={scope ? [scope, ...cluster] : cluster}
            pickResource={scope === undefined}
            onDone={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

/**
 * Opens the annotation form for `scope`, or with a resource search without one.
 */
export default function AddAnnotationButton({ scope }: { scope?: ResourceScope | undefined }) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        onClick={() => setOpen(true)}
      >
        <MessageSquarePlus className="size-3.5" />
        Add annotation
      </Button>

      <AddAnnotationDialog
        open={open}
        onOpenChange={setOpen}
        scope={scope}
      />
    </>
  );
}
