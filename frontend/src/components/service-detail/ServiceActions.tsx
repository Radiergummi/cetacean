import { api } from "../../api/client";
import type { Service } from "../../api/types";
import { Spinner } from "../Spinner";
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
import { ImageIcon, RefreshCw, RotateCcw } from "lucide-react";
import { useState } from "react";

export function ServiceActions({ service, serviceId }: { service: Service; serviceId: string }) {
  const currentImage = service.Spec.TaskTemplate.ContainerSpec.Image;
  const imageWithoutDigest = currentImage.replace(/@sha256:[a-f0-9]+$/, "");

  const [imageOpen, setImageOpen] = useState(false);
  const [imageValue, setImageValue] = useState("");
  const [imageLoading, setImageLoading] = useState(false);
  const [imageError, setImageError] = useState<string | null>(null);

  const [rollbackLoading, setRollbackLoading] = useState(false);
  const [rollbackError, setRollbackError] = useState<string | null>(null);

  const [restartLoading, setRestartLoading] = useState(false);
  const [restartError, setRestartError] = useState<string | null>(null);

  const canRollback = !!service.PreviousSpec;

  function handleImageOpenChange(open: boolean) {
    if (open) {
      setImageValue(imageWithoutDigest);
      setImageError(null);
    } else {
      setImageError(null);
    }
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
      setImageError(error instanceof Error ? error.message : "Failed to update image");
    } finally {
      setImageLoading(false);
    }
  }

  async function executeRollback() {
    setRollbackLoading(true);
    setRollbackError(null);

    try {
      await api.rollbackService(serviceId);
    } catch (error) {
      setRollbackError(error instanceof Error ? error.message : "Failed to rollback");
    } finally {
      setRollbackLoading(false);
    }
  }

  async function executeRestart() {
    setRestartLoading(true);
    setRestartError(null);

    try {
      await api.restartService(serviceId);
    } catch (error) {
      setRestartError(error instanceof Error ? error.message : "Failed to restart");
    } finally {
      setRestartLoading(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* Update Image */}
      <Popover open={imageOpen} onOpenChange={handleImageOpenChange} modal>
        <PopoverTrigger
          render={
            <Button variant="outline" size="sm">
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

      {/* Rollback */}
      <div className="flex flex-col items-start gap-1">
        <AlertDialog>
          <AlertDialogTrigger
            render={
              <Button
                variant="outline"
                size="sm"
                disabled={!canRollback || rollbackLoading}
                title={canRollback ? "Rollback to previous spec" : "No previous spec available"}
              >
                {rollbackLoading ? (
                  <Spinner className="size-3" />
                ) : (
                  <RotateCcw className="size-3.5" />
                )}
                Rollback
              </Button>
            }
          />
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Rollback service?</AlertDialogTitle>
              <AlertDialogDescription>
                This will rollback the service to its previous specification.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction onClick={() => void executeRollback()}>
                Rollback
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
        {rollbackError && (
          <p className="text-xs text-red-600 dark:text-red-400">{rollbackError}</p>
        )}
      </div>

      {/* Restart */}
      <div className="flex flex-col items-start gap-1">
        <AlertDialog>
          <AlertDialogTrigger
            render={
              <Button variant="outline" size="sm" disabled={restartLoading}>
                {restartLoading ? (
                  <Spinner className="size-3" />
                ) : (
                  <RefreshCw className="size-3.5" />
                )}
                Restart
              </Button>
            }
          />
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Restart service?</AlertDialogTitle>
              <AlertDialogDescription>
                This triggers a rolling restart of all tasks.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction onClick={() => void executeRestart()}>
                Restart
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
        {restartError && (
          <p className="text-xs text-red-600 dark:text-red-400">{restartError}</p>
        )}
      </div>
    </div>
  );
}
