import type { AttestedKind } from "@/api/client";
import { useProvenance } from "@/hooks/useProvenance";
import { BadgeCheck } from "lucide-react";

const attestedMessage =
  "This image carries build attestations. They are what the image says about itself; nothing checks them.";

/**
 * Marks an image whose registry holds build attestations for it.
 */
export function AttestedBadge({ kind, id }: { kind: AttestedKind; id: string }) {
  const { data } = useProvenance(kind, id);

  if (data?.status !== "available") {
    return null;
  }

  return (
    <span
      className="inline-flex shrink-0 text-status-info"
      title={attestedMessage}
    >
      <BadgeCheck
        className="size-4"
        aria-hidden="true"
      />
      <span className="sr-only">Attested: {attestedMessage}</span>
    </span>
  );
}
