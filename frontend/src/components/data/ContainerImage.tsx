import { imageRegistryUrl } from "../../lib/imageUrl";
import InfoCard from "../InfoCard";

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

export default function ContainerImage({
  image,
  label = "Image",
}: {
  image?: string;
  label?: string;
}) {
  if (!image) {
    return null;
  }

  const display = image.split("@")[0];
  const href = imageRegistryUrl(image);
  const favicon = registryFavicon(image);

  const inner = (
    <>
      {favicon && (
        <img
          src={favicon}
          alt=""
          className="h-4 w-4 shrink-0"
        />
      )}
      {display}
    </>
  );

  const value = href ? (
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

  return (
    <InfoCard
      label={label}
      value={value}
    />
  );
}
