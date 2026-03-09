/**
 * Parse a Docker image reference and return a URL to its registry page.
 * Handles Docker Hub (official + user), ghcr.io, quay.io, and gcr.io.
 * Returns null for unknown/private registries.
 */
export function imageRegistryUrl(image: string): string | null {
  // Strip digest (@sha256:...) and tag (:tag)
  const withoutDigest = image.split("@")[0];
  const [namePart] = withoutDigest.split(":");

  const segments = namePart.split("/");

  // No slashes or "library/x" → Docker Hub official image
  if (segments.length === 1) {
    return `https://hub.docker.com/_/${segments[0]}`;
  }

  // Check if first segment looks like a registry hostname (contains a dot or port)
  const firstSegment = segments[0];
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
