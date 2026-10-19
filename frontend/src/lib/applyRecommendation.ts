import { api } from "@/api/client";
import type { Recommendation } from "@/api/types";

/**
 * Applies the fix action for a single recommendation.
 * Does nothing if the recommendation has no fixAction or suggested value.
 */
export async function applyRecommendation(recommendation: Recommendation): Promise<void> {
  const { fixAction, targetId, suggested, resource, category } = recommendation;

  if (!fixAction || suggested == null) {
    return;
  }

  if (fixAction.includes("/resources")) {
    const isOverProvisioned = category === "over-provisioned";
    const field = isOverProvisioned ? "Reservations" : "Limits";
    const key = resource === "memory" ? "MemoryBytes" : "NanoCPUs";
    await api.patchServiceResources(targetId, { [field]: { [key]: Math.round(suggested) } });
  } else if (fixAction.includes("/scale")) {
    await api.scaleService(targetId, Math.round(suggested));
  } else if (fixAction.includes("/availability")) {
    await api.updateNodeAvailability(targetId, "drain");
  }
}

/**
 * The resource a recommendation's fix acts on and the sub-resource it writes,
 * read from its fix action ("PUT /services/{id}/scale"), so a page can offer
 * the fix only where the target links that sub-resource as editable.
 */
export function fixTarget({ fixAction, targetId }: Recommendation): {
  path: string;
  subResource: string;
} | null {
  const [, template] = fixAction?.split(" ") ?? [];
  const [, collection, , subResource] = template?.split("/") ?? [];

  return collection && subResource ? { path: `/${collection}/${targetId}`, subResource } : null;
}
