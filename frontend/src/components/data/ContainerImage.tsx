import InfoCard from "../InfoCard";
import { api } from "@/api/client";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useAsyncAction } from "@/hooks/useAsyncAction";
import { imageRegistryUrl } from "@/lib/imageUrl";
import { Pencil } from "lucide-react";
import { useState } from "react";

function registryFavicon(image: string): string | null {
  const namePart = image.split("@")[0].split(":")[0];
  const segments = namePart.split("/");
  const first = segments[0];

  if (!first.includes(".") && !first.includes(":")) {
    return "https://hub.docker.com/favicon.ico";
  }

  if (first === "docker.io" || first === "registry-1.docker.io") {
    return "https://hub.docker.com/favicon.ico";
  }

  if (first === "ghcr.io") {
    return "https://github.com/favicon.ico";
  }

  if (first === "quay.io") {
    return "https://quay.io/static/img/quay_favicon.png";
  }

  if (first === "gcr.io" || first.endsWith(".gcr.io")) {
    return "https://cloud.google.com/favicon.ico";
  }

  return null;
}

function ImageUpdatePopover({
  serviceId,
  currentImage,
}: {
  serviceId: string;
  currentImage: string;
}) {
  const imageWithoutDigest = currentImage.replace(/@sha256:[a-f0-9]+$/, "");
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const update = useAsyncAction({ toast: true });

  function handleOpenChange(next: boolean) {
    if (next) {
      setValue(imageWithoutDigest);
    }

    setValidationError(null);
    setOpen(next);
  }

  async function submit() {
    const trimmed = value.trim();

    if (!trimmed) {
      setValidationError("Enter an image name");

      return;
    }

    setValidationError(null);

    await update.execute(async () => {
      await api.updateServiceImage(serviceId, trimmed);
      setOpen(false);
    }, "Failed to update image");
  }

  return (
    <Popover
      open={open}
      onOpenChange={handleOpenChange}
      modal
    >
      <PopoverTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            title="Update image"
          >
            <Pencil className="size-3.5" />
          </Button>
        }
      />
      <PopoverContent
        className="w-80"
        align="end"
      >
        <p className="mb-1 text-xs font-medium text-muted-foreground">New image</p>
        <Input
          value={value}
          onChange={(event) => setValue(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              void submit();
            }
          }}
          placeholder="image:tag"
          className="mb-2 font-mono"
          autoFocus
        />
        {validationError && (
          <p className="mb-2 text-xs text-red-600 dark:text-red-400">{validationError}</p>
        )}
        <div className="flex gap-2">
          <Button
            onClick={() => void submit()}
            disabled={update.loading}
            size="sm"
            className="flex-1"
          >
            {update.loading && <Spinner className="size-3" />}
            Update
          </Button>
          <Button
            variant="outline"
            onClick={() => setOpen(false)}
            disabled={update.loading}
            size="sm"
            className="flex-1"
          >
            Cancel
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

export default function ContainerImage({
  image,
  label = "Image",
  serviceId,
  canEdit = false,
}: {
  image?: string;
  label?: string;
  serviceId?: string;
  canEdit?: boolean;
}) {
  if (!image) {
    return null;
  }

  const display = image.split("@")[0].replace(/^(docker\.io|registry-1\.docker\.io)\//, "");
  const href = imageRegistryUrl(image);
  const favicon = registryFavicon(image);

  const inner = (
    <>
      {favicon && (
        <img
          src={favicon}
          alt=""
          aria-hidden="true"
          className="h-4 w-4 shrink-0"
        />
      )}
      {display}
    </>
  );

  const link = href ? (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-1.5 text-link hover:underline"
    >
      {inner}
    </a>
  ) : (
    <span className="inline-flex items-center gap-1.5">{inner}</span>
  );

  const value =
    serviceId && canEdit ? (
      <>
        {link}
        <ImageUpdatePopover
          serviceId={serviceId}
          currentImage={image}
        />
      </>
    ) : (
      link
    );

  return (
    <InfoCard
      label={label}
      value={value}
    />
  );
}
