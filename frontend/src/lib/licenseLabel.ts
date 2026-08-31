import type { LicenseEntry } from "@/api/types";

/**
 * The label a license is known by everywhere it is shown. The filter matches
 * on it, the dropdown counts it, the badge shows it, and the text dialog
 * titles itself with it — they have to agree exactly or the filter silently
 * stops matching what the badge says.
 */
export function licenseLabel({ id, name }: LicenseEntry): string {
  return id || name || "Unknown";
}
