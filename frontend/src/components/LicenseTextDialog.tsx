import { api } from "@/api/client";
import type { LicenseComponent } from "@/api/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

interface LicenseTextDialogProps {
  component: LicenseComponent;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * Shows one dependency's verbatim license text, and its NOTICE when it ships
 * one. Texts are fetched on open rather than with the page: the pool is some
 * 300KB and most visitors open none of it.
 */
export default function LicenseTextDialog({
  component,
  open,
  onOpenChange,
}: LicenseTextDialogProps) {
  const { textId, noticeId } = component;

  const license = useQuery({
    queryKey: ["licenseText", textId],
    queryFn: ({ signal }) => api.licenseText(textId!, signal),
    enabled: open && !!textId,
    staleTime: Infinity,
  });

  const notice = useQuery({
    queryKey: ["licenseText", noticeId],
    queryFn: ({ signal }) => api.licenseText(noticeId!, signal),
    enabled: open && !!noticeId,
    staleTime: Infinity,
  });

  const identifiers = useMemo(
    () =>
      component.licenses
        .map(({ id, name }) => id || name)
        .filter(Boolean)
        .join(", "),
    [component.licenses],
  );

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
    >
      <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{component.name}</DialogTitle>
          <DialogDescription>
            {identifiers}
            {component.version ? ` · ${component.version}` : ""}
          </DialogDescription>
        </DialogHeader>

        {license.isError ? (
          <p className="text-sm text-destructive">Could not load the license text.</p>
        ) : (
          <pre className="font-mono text-xs whitespace-pre-wrap text-muted-foreground">
            {license.data ?? "Loading…"}
          </pre>
        )}

        {noticeId && (
          <section className="border-t pt-4">
            <h3 className="mb-2 text-sm font-medium">NOTICE</h3>
            {notice.isError ? (
              <p className="text-sm text-destructive">Could not load the notice.</p>
            ) : (
              <pre className="font-mono text-xs whitespace-pre-wrap text-muted-foreground">
                {notice.data ?? "Loading…"}
              </pre>
            )}
          </section>
        )}
      </DialogContent>
    </Dialog>
  );
}
