/**
 * Parse a Docker placement constraint expression into a human-readable label.
 *
 * Handles node.role, node.hostname, node.id, node.platform.os/arch,
 * node.labels.*, and engine.labels.* fields.
 */
export function humanizeConstraint(raw: string): { label: string; exclude: boolean } | null {
  const match = raw.match(/^(.+?)\s*(==|!=)\s*(.+)$/);

  if (!match) {
    return null;
  }

  const [, field, op, value] = match;
  const exclude = op === "!=";

  if (field === "node.role") {
    if (value === "manager" && !exclude) {
      return { label: "Manager nodes only", exclude };
    }

    if (value === "worker" && !exclude) {
      return { label: "Worker nodes only", exclude };
    }

    if (value === "manager" && exclude) {
      return { label: "Exclude manager nodes", exclude };
    }

    if (value === "worker" && exclude) {
      return { label: "Exclude worker nodes", exclude };
    }
  }

  if (field === "node.hostname") {
    return {
      label: exclude ? `Exclude node ${value}` : `Node: ${value}`,
      exclude,
    };
  }

  if (field === "node.id") {
    return {
      label: exclude ? `Exclude node ID ${value}` : `Node ID: ${value}`,
      exclude,
    };
  }

  if (field === "node.platform.os") {
    return {
      label: exclude ? `Exclude OS ${value}` : `OS: ${value}`,
      exclude,
    };
  }

  if (field === "node.platform.arch") {
    return {
      label: exclude ? `Exclude arch ${value}` : `Arch: ${value}`,
      exclude,
    };
  }

  if (field.startsWith("node.labels.")) {
    const key = field.slice("node.labels.".length);

    return {
      label: exclude ? `${key} \u2260 ${value}` : `${key} = ${value}`,
      exclude,
    };
  }

  if (field.startsWith("engine.labels.")) {
    const key = field.slice("engine.labels.".length);

    return {
      label: exclude ? `engine ${key} \u2260 ${value}` : `engine ${key} = ${value}`,
      exclude,
    };
  }

  return null;
}
