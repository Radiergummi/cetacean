import { api } from "../../api/client";
import type { Service } from "../../api/types";
import { Spinner } from "../Spinner";
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

  function openImage() {
    setImageValue(imageWithoutDigest);
    setImageError(null);
    setImageOpen(true);
  }

  function cancelImage() {
    setImageOpen(false);
    setImageError(null);
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

  async function handleRollback() {
    if (!window.confirm("Are you sure you want to rollback this service?")) {
      return;
    }

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

  async function handleRestart() {
    if (
      !window.confirm(
        "Are you sure you want to restart this service? This triggers a rolling restart.",
      )
    ) {
      return;
    }

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
      <div className="relative">
        <button
          type="button"
          onClick={openImage}
          className="inline-flex items-center gap-1.5 rounded border px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:opacity-50"
        >
          <ImageIcon className="h-3.5 w-3.5" />
          Update Image
        </button>
        {imageOpen && (
          <div className="absolute top-full left-0 z-50 mt-1 w-80 rounded-lg border bg-card p-3 shadow-lg">
            <p className="mb-1 text-xs font-medium text-muted-foreground">New image</p>
            <p
              className="mb-2 truncate font-mono text-xs text-muted-foreground"
              title={currentImage}
            >
              Current: {imageWithoutDigest}
            </p>
            <input
              type="text"
              value={imageValue}
              onChange={(e) => setImageValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  void submitImage();
                }
                if (e.key === "Escape") {
                  cancelImage();
                }
              }}
              placeholder="image:tag"
              className="mb-2 w-full rounded border bg-background px-2 py-1 font-mono text-sm focus:ring-1 focus:ring-ring focus:outline-none"
              autoFocus
            />
            {imageError && (
              <p className="mb-2 text-xs text-red-600 dark:text-red-400">{imageError}</p>
            )}
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => void submitImage()}
                disabled={imageLoading}
                className="flex flex-1 items-center justify-center gap-1 rounded bg-primary px-2 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
              >
                {imageLoading && <Spinner className="size-3" />}
                Update
              </button>
              <button
                type="button"
                onClick={cancelImage}
                disabled={imageLoading}
                className="flex-1 rounded border px-2 py-1 text-xs font-medium disabled:opacity-50"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Rollback */}
      <div className="flex flex-col items-start gap-1">
        <button
          type="button"
          onClick={() => void handleRollback()}
          disabled={!canRollback || rollbackLoading}
          title={canRollback ? "Rollback to previous spec" : "No previous spec available"}
          className="inline-flex items-center gap-1.5 rounded border px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          {rollbackLoading ? <Spinner className="size-3" /> : <RotateCcw className="h-3.5 w-3.5" />}
          Rollback
        </button>
        {rollbackError && <p className="text-xs text-red-600 dark:text-red-400">{rollbackError}</p>}
      </div>

      {/* Restart */}
      <div className="flex flex-col items-start gap-1">
        <button
          type="button"
          onClick={() => void handleRestart()}
          disabled={restartLoading}
          className="inline-flex items-center gap-1.5 rounded border px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:opacity-50"
        >
          {restartLoading ? <Spinner className="size-3" /> : <RefreshCw className="h-3.5 w-3.5" />}
          Restart
        </button>
        {restartError && <p className="text-xs text-red-600 dark:text-red-400">{restartError}</p>}
      </div>
    </div>
  );
}
