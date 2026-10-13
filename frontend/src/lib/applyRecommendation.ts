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
 * The method a recommendation's fix needs, and the path of the resource it
 * acts on, read from its fix action ("PUT /services/{id}/scale"), so a page
 * can offer the fix only where the target's Allow header carries the method.
 */
export function fixTarget({ fixAction, targetId }: Recommendation): {
  method: string;
  path: string;
} | null {
  const [method, template] = fixAction?.split(" ") ?? [];

  if (!method || !template) {
    return null;
  }

  const [, collection] = template.split("/");

  return collection ? { method, path: `/${collection}/${targetId}` } : null;
}
