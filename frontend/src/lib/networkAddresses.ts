import type { NetworkAddresses } from "@/api/types";
import type { StatusTone } from "@/lib/statusColor";

/**
 * The tone of a network's address use, at the thresholds the
 * `network-addresses-low` recommendation raises at; null below them.
 */
export function addressTone({ usable, used }: NetworkAddresses): StatusTone | null {
  if (usable === 0) {
    return null;
  }

  if (used * 100 >= usable * 95) {
    return "danger";
  }

  if (used * 100 >= usable * 80) {
    return "warning";
  }

  return null;
}
