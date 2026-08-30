import type { ReactNode } from "react";

export interface Crumb {
  label: ReactNode;
  to?: string | undefined;
}

interface ResourceCrumbsConfig {
  /** Plural label of the resource's own list page, e.g. "Services". */
  listLabel: string;
  /** Path of that list page, e.g. "/services". */
  listPath: string;
  /** The resource's full Docker name, stack prefix and all. */
  name: string;
  /** Value of the com.docker.stack.namespace label, when the resource has one. */
  stack?: string | undefined;
  /** The resource's own detail path. Only needed when trail is non-empty. */
  to?: string | undefined;
  /** Crumbs below the resource itself, e.g. a task or a service sub-resource. */
  trail?: Crumb[] | undefined;
}

/**
 * Builds the breadcrumb trail for a resource detail page.
 *
 * A resource deployed as part of a stack is reached through its stack —
 * "Stacks › monitoring › prometheus" — because that is where it actually lives
 * and how the rest of the UI presents it. Everything else is reached through
 * its own list page: "Volumes › scratch".
 */
export function resourceBreadcrumbs({
  listLabel,
  listPath,
  name,
  stack,
  to,
  trail = [],
}: ResourceCrumbsConfig): Crumb[] {
  const leaf: Crumb = {
    label: stripStackPrefix(name, stack),
    // The resource is only a link when something sits below it; the last crumb
    // is the page you are already on.
    to: trail.length > 0 ? to : undefined,
  };

  if (!stack) {
    return [{ label: listLabel, to: listPath }, leaf, ...trail];
  }

  return [
    { label: "Stacks", to: "/stacks" },
    { label: stack, to: `/stacks/${encodeURIComponent(stack)}` },
    leaf,
    ...trail,
  ];
}

/**
 * Drops the "<stack>_" prefix Docker gives a stack's resources, so the name
 * doesn't repeat the stack crumb standing right next to it. A resource can
 * join a stack by label without being named for it, so the prefix is only
 * removed when it is actually there — and never guessed at when the resource
 * belongs to no stack, where an underscore is just part of the name.
 */
export function stripStackPrefix(name: string, stack?: string | undefined): string {
  if (stack && name.startsWith(`${stack}_`)) {
    return name.slice(stack.length + 1);
  }

  return name;
}
