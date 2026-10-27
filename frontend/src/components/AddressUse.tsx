import type { NetworkAddresses } from "../api/types";
import { addressTone } from "../lib/networkAddresses";
import { toneText } from "../lib/statusColor";

/**
 * A network's addresses in use against those usable, toned at the thresholds
 * the `network-addresses-low` recommendation raises at.
 */
export default function AddressUse({ addresses }: { addresses: NetworkAddresses }) {
  const tone = addressTone(addresses);

  return (
    <span className={`tabular-nums ${tone ? toneText[tone] : ""}`}>
      {addresses.used} / {addresses.usable}
    </span>
  );
}
