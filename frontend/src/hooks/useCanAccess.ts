import { useAuth } from "@/hooks/useAuth";

/**
 * Checks whether the current user has a specific permission on a resource.
 * Uses simple glob matching (only `*` wildcard) against the permissions map
 * from the whoami response.
 *
 * Returns true if no permissions are configured (no ACL policy active).
 */
export function useCanAccess(resource: string, permission: string): boolean {
  const { identity } = useAuth();

  if (!identity || !identity.permissions) {
    return true;
  }

  const permissions = identity.permissions;

  for (const [pattern, grants] of Object.entries(permissions)) {
    if (grants.includes(permission) || (permission === "read" && grants.includes("write"))) {
      if (matchGlob(pattern, resource)) {
        return true;
      }
    }
  }

  return false;
}

/**
 * Simple glob match supporting only `*` wildcards.
 * Bare `*` matches everything.
 */
function matchGlob(pattern: string, value: string): boolean {
  if (pattern === "*") {
    return true;
  }

  const escaped = pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*");
  const regex = new RegExp(`^${escaped}$`);

  return regex.test(value);
}
