import { useAuth } from "@/hooks/useAuth";

/**
 * Names the relationships a grant can reach a resource through, mirroring the
 * server's own resolution (`grantMatchesResource`): a task inherits from its
 * parent service, and a resource inherits from the stack it belongs to. Without
 * these, a grant on `stack:web` looks like it covers nothing but the stack
 * itself, and the UI hides actions the server would allow.
 *
 * Both are names, not IDs — grants are written against display names.
 */
export type ResourceOwnership = {
  /**
   * Name of the parent service. Only meaningful for a task.
   */
  service?: string | undefined;

  /**
   * Name of the stack this resource — or, for a task, its parent service —
   * belongs to.
   */
  stack?: string | undefined;
};

/**
 * Checks whether the current user has a specific permission on a resource.
 *
 * Returns true if no permissions are configured (no ACL policy active). This
 * decides whether to render an affordance, never whether an action is allowed:
 * the server checks every mutation again, so being too permissive here shows a
 * button that returns 403, and being too strict hides one that would work.
 */
export function useCanAccess(
  resource: string,
  permission: string,
  ownership?: ResourceOwnership,
): boolean {
  const { identity } = useAuth();

  return canAccess(identity?.permissions, resource, permission, ownership);
}

/**
 * The pure half of useCanAccess, over the permissions map from the whoami
 * response.
 */
export function canAccess(
  permissions: Record<string, string[]> | undefined,
  resource: string,
  permission: string,
  ownership: ResourceOwnership = {},
): boolean {
  if (!permissions) {
    return true;
  }

  // A grant covers a resource directly, or through the resource it inherits
  // from. The order matches the server's: the resource itself, its parent
  // service, then the stack either belongs to. Which inheritance applies
  // depends on the resource's own type, so a caller passing ownership a
  // resource cannot actually have widens nothing.
  const resourceType = splitOnce(resource)?.[0] ?? "";
  const covered = [resource];

  if (ownership.service && resourceType === "task") {
    covered.push(`service:${ownership.service}`);
  }

  if (ownership.stack && belongsToAStack(resourceType)) {
    covered.push(`stack:${ownership.stack}`);
  }

  for (const [pattern, grants] of Object.entries(permissions)) {
    if (!grantsPermission(grants, permission)) {
      continue;
    }

    if (covered.some((candidate) => matchResource(pattern, candidate))) {
      return true;
    }
  }

  return false;
}

/**
 * The resource types that can carry a stack namespace label, mirroring the
 * server's StackOf. Nodes, plugins and the swarm itself belong to no stack, so
 * no stack grant reaches them however the caller describes them.
 */
const stackMemberTypes = ["service", "config", "secret", "network", "volume"] as const;

/**
 * Reports whether a resource of this type can belong to a stack. A task does,
 * through its parent service.
 */
function belongsToAStack(resourceType: string): boolean {
  if (resourceType === "task") {
    return true;
  }

  return (stackMemberTypes as readonly string[]).includes(resourceType);
}

/**
 * Reports whether a grant's permission list covers the requested permission.
 * Write implies read, as it does server-side.
 */
function grantsPermission(grants: string[], permission: string): boolean {
  if (grants.includes(permission)) {
    return true;
  }

  return permission === "read" && grants.includes("write");
}

/**
 * Matches a `type:pattern` grant expression against a `type:name` resource.
 * Mirrors the server's matchResource: the type must match exactly unless the
 * expression's type is `*`, the name is matched as a glob, and an empty name
 * never matches. Bare `*` matches everything.
 */
function matchResource(expression: string, resource: string): boolean {
  if (expression === "*") {
    return true;
  }

  const expressionParts = splitOnce(expression);
  const resourceParts = splitOnce(resource);

  if (!expressionParts || !resourceParts) {
    return false;
  }

  const [expressionType, expressionPattern] = expressionParts;
  const [resourceType, resourceName] = resourceParts;

  if (expressionType !== "*" && expressionType !== resourceType) {
    return false;
  }

  if (resourceName === "") {
    return false;
  }

  return matchGlob(expressionPattern, resourceName);
}

/**
 * Splits `type:name` on the first colon, or returns null when there is none.
 */
function splitOnce(value: string): [string, string] | null {
  const separator = value.indexOf(":");

  if (separator === -1) {
    return null;
  }

  return [value.slice(0, separator), value.slice(separator + 1)];
}

/**
 * Glob match supporting `*` (any run of characters) and `?` (exactly one),
 * the wildcards the server's path.Match accepts. Character classes are not
 * supported and are matched literally.
 */
function matchGlob(pattern: string, value: string): boolean {
  const escaped = pattern.replace(/[.+^${}()|[\]\\?*]/g, (character) => {
    if (character === "*") {
      return ".*";
    }

    if (character === "?") {
      return ".";
    }

    return `\\${character}`;
  });

  return new RegExp(`^${escaped}$`).test(value);
}
