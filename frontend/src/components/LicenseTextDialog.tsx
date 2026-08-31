import { api } from "@/api/client";
import type { LicenseComponent } from "@/api/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { licenseLabel } from "@/lib/licenseLabel";
import { useQuery } from "@tanstack/react-query";

interface LicenseTextDialogProps {
  component: LicenseComponent;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

interface TextPaneProps {
  id: string;
  enabled: boolean;
  errorMessage: string;
}

/**
 * One pooled text, fetched by its content-addressed id. The id is a hash of
 * the bytes, so the text behind it can never change — hence the infinite stale
 * time.
 */
function TextPane({ id, enabled, errorMessage }: TextPaneProps) {
  const { data, isError } = useQuery({
    queryKey: ["licenseText", id],
    queryFn: ({ signal }) => api.licenseText(id, signal),
    enabled,
    staleTime: Infinity,
  });

  if (isError) {
    return <p className="text-sm text-destructive">{errorMessage}</p>;
  }

  return (
    <pre className="font-mono text-xs whitespace-pre-wrap text-muted-foreground">
      {data ?? "Loading…"}
    </pre>
  );
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

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
    >
      <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{component.name}</DialogTitle>
          <DialogDescription>
            {component.licenses.map(licenseLabel).join(", ")}
            {component.version ? ` · ${component.version}` : ""}
          </DialogDescription>
        </DialogHeader>

        {textId && (
          <TextPane
            id={textId}
            enabled={open}
            errorMessage="Could not load the license text."
          />
        )}

        {noticeId && (
          <section className="border-t pt-4">
            <h3 className="mb-2 text-sm font-medium">NOTICE</h3>
            <TextPane
              id={noticeId}
              enabled={open}
              errorMessage="Could not load the notice."
            />
          </section>
        )}
      </DialogContent>
    </Dialog>
  );
}
