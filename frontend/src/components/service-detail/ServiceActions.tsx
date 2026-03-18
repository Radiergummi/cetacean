import { api } from "@/api/client";
import type { Service } from "@/api/types";
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
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { getErrorMessage } from "@/lib/utils";
import type { LucideIcon } from "lucide-react";
import { ImageIcon, RefreshCw, RotateCcw } from "lucide-react";
import { useState } from "react";

function ConfirmAction({
  icon: Icon,
  label,
  title,
  description,
  disabled,
  disabledTitle,
  loading,
  error,
  onConfirm,
}: {
  icon: LucideIcon;
  label: string;
  title: string;
  description: string;
  disabled?: boolean;
  disabledTitle?: string;
  loading: boolean;
  error: string | null;
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
              disabled={disabled || loading}
              title={disabled ? disabledTitle : undefined}
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
      {error && <p className="text-xs text-red-600 dark:text-red-400">{error}</p>}
    </div>
  );
}

export function ServiceActions({ service, serviceId }: { service: Service; serviceId: string }) {
  const currentImage = service.Spec.TaskTemplate.ContainerSpec.Image;
  const imageWithoutDigest = currentImage.replace(/@sha256:[a-f0-9]+$/, "");

  const [imageOpen, setImageOpen] = useState(false);
  const [imageValue, setImageValue] = useState("");
  const [imageLoading, setImageLoading] = useState(false);
  const [imageError, setImageError] = useState<string | null>(null);

  const rollback = useAsyncAction();
  const restart = useAsyncAction();

  const canRollback = !!service.PreviousSpec;

  function handleImageOpenChange(open: boolean) {
    if (open) {
      setImageValue(imageWithoutDigest);
    }
    setImageError(null);
    setImageOpen(open);
  }

  async function submitImage() {
    const trimmed = imageValue.trim();

    if (!trimmed) {
      setImageError("Enter an image name");
      return;
    }

    setImageLoading(true);
    setImageError(null);

    try {
      await api.updateServiceImage(serviceId, trimmed);
      setImageOpen(false);
    } catch (error) {
      setImageError(getErrorMessage(error, "Failed to update image"));
    } finally {
      setImageLoading(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* Update Image */}
      <Popover
        open={imageOpen}
        onOpenChange={handleImageOpenChange}
        modal
      >
        <PopoverTrigger
          render={
            <Button
              variant="outline"
              size="sm"
            >
              <ImageIcon className="size-3.5" />
              Update Image
            </Button>
          }
        />
        <PopoverContent className="w-80">
          <p className="mb-1 text-xs font-medium text-muted-foreground">New image</p>
          <p
            className="mb-2 truncate font-mono text-xs text-muted-foreground"
            title={currentImage}
          >
            Current: {imageWithoutDigest}
          </p>
          <Input
            value={imageValue}
            onChange={(event) => setImageValue(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                void submitImage();
              }
            }}
            placeholder="image:tag"
            className="mb-2 font-mono"
            autoFocus
          />
          {imageError && (
            <p className="mb-2 text-xs text-red-600 dark:text-red-400">{imageError}</p>
          )}
          <div className="flex gap-2">
            <Button
              onClick={() => void submitImage()}
              disabled={imageLoading}
              size="sm"
              className="flex-1"
            >
              {imageLoading && <Spinner className="size-3" />}
              Update
            </Button>
            <Button
              variant="outline"
              onClick={() => setImageOpen(false)}
              disabled={imageLoading}
              size="sm"
              className="flex-1"
            >
              Cancel
            </Button>
          </div>
        </PopoverContent>
      </Popover>

      <ConfirmAction
        icon={RotateCcw}
        label="Rollback"
        title="Rollback service?"
        description="This will rollback the service to its previous specification."
        disabled={!canRollback}
        disabledTitle="No previous spec available"
        loading={rollback.loading}
        error={rollback.error}
        onConfirm={() =>
          void rollback.execute(() => api.rollbackService(serviceId), "Failed to rollback")
        }
      />

      <ConfirmAction
        icon={RefreshCw}
        label="Restart"
        title="Restart service?"
        description="This triggers a rolling restart of all tasks."
        loading={restart.loading}
        error={restart.error}
        onConfirm={() =>
          void restart.execute(() => api.restartService(serviceId), "Failed to restart")
        }
      />
    </div>
  );
}
