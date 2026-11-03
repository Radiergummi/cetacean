import AnnotationForm from "./AnnotationForm";
import type { AnnotationScope } from "@/api/types";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { useClusterAnnotatable } from "@/hooks/useClusterAnnotatable";
import { MessageSquarePlus } from "lucide-react";
import { useState } from "react";

/**
 * Opens the annotation form in a dialog. With a resource it is that resource's
 * note; without one, as on History, the form searches for a resource.
 */
export default function AddAnnotationButton({
  scope,
}: {
  scope?: Exclude<AnnotationScope, "cluster"> | undefined;
}) {
  const [open, setOpen] = useState(false);
  const [openedAt, setOpenedAt] = useState(0);
  const clusterWide = useClusterAnnotatable();
  const cluster: AnnotationScope[] = clusterWide ? ["cluster"] : [];

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          setOpenedAt(Math.floor(Date.now() / 1000));
        }

        setOpen(next);
      }}
    >
      <DialogTrigger
        render={
          <Button
            variant="outline"
            size="sm"
          >
            <MessageSquarePlus className="size-3.5" />
            Add annotation
          </Button>
        }
      />

      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add annotation</DialogTitle>
          <DialogDescription>
            A note about a moment or a range, for everyone to see.
          </DialogDescription>
        </DialogHeader>

        {open && (
          <AnnotationForm
            scopes={scope ? [scope, ...cluster] : cluster}
            pickResource={scope === undefined}
            initial={{ from: openedAt }}
            onDone={() => setOpen(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
