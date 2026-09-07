/**
 * Parse a Docker image reference and return a URL to its registry page.
 * Handles Docker Hub (official and user), ghcr.io, quay.io, and gcr.io.
 * Returns null for unknown/private registries.
 */
export function imageRegistryUrl(image: string): string | null {
  // Strip digest (`@sha256:...`), then tag (`:tag`). A tag can only appear in
  // the final path segment, so splitting on the first colon would eat the port
  // of a registry like `registry.example.com:5000/team/image` and leave the
  // hostname looking like an official Docker Hub image.
  const withoutDigest = image.split("@")[0] ?? image;
  const lastSlash = withoutDigest.lastIndexOf("/");
  const lastColon = withoutDigest.lastIndexOf(":");
  const namePart = lastColon > lastSlash ? withoutDigest.slice(0, lastColon) : withoutDigest;

  const segments = namePart.split("/");
  const firstSegment = segments[0] ?? "";

  // No slashes or "library/x" → Docker Hub official image
  if (segments.length === 1) {
    return `https://hub.docker.com/_/${firstSegment}`;
  }

  // Check if first segment looks like a registry hostname (contains a dot or port)
  const isRegistry = firstSegment.includes(".") || firstSegment.includes(":");

  if (!isRegistry) {
    // Two segments, no registry → Docker Hub user image (e.g. "myuser/myimage")
    return `https://hub.docker.com/r/${segments.join("/")}`;
  }

  const registry = firstSegment;
  const repo = segments.slice(1).join("/");

  if (registry === "docker.io" || registry === "registry-1.docker.io") {
    if (repo.startsWith("library/")) {
      return `https://hub.docker.com/_/${repo.slice("library/".length)}`;
    }
    return `https://hub.docker.com/r/${repo}`;
  }

  if (registry === "ghcr.io") {
    return `https://ghcr.io/${repo}`;
  }

  if (registry === "quay.io") {
    return `https://quay.io/repository/${repo}`;
  }

  if (registry === "gcr.io" || registry.endsWith(".gcr.io")) {
    return `https://${registry}/${repo}`;
  }

  return null;
}
