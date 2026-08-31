import { stripStackPrefix } from "./parseStackLabels";
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
 * Where a resource is reached from, and so where to go once it is gone: its
 * stack when it belongs to one, its own list page otherwise. The breadcrumbs
 * and the post-removal redirect have to agree on this, encoding included.
 */
export function resourceParentPath({
  listPath,
  stack,
}: {
  listPath: string;
  stack?: string | undefined;
}): string {
  return stack ? `/stacks/${encodeURIComponent(stack)}` : listPath;
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
    { label: stack, to: resourceParentPath({ listPath, stack }) },
    leaf,
    ...trail,
  ];
}
