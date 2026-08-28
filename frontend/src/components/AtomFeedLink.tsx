import { apiPath } from "@/lib/basePath";
import { useLocation } from "react-router-dom";

/**
 * Resource types that have Atom feed support on both list and detail routes.
 */
const feedResources = new Set([
  "nodes",
  "services",
  "tasks",
  "stacks",
  "configs",
  "secrets",
  "networks",
  "volumes",
]);

/**
 * Standalone routes (not resource CRUD) that have Atom feeds.
 */
const feedRoutes: Record<string, string> = {
  "/history": "History",
  "/recommendations": "Recommendations",
};

/**
 * Determines the Atom feed href and title for a given path and search string.
 * Exported for testing.
 */
export function feedForPath(
  pathname: string,
  search: string,
): { href: string; title: string } | null {
  // Check standalone routes first.
  for (const [route, title] of Object.entries(feedRoutes)) {
    if (pathname === route) {
      return { href: apiPath(route), title };
    }
  }

  // Check /search with a query — include the query string so feed readers
  // hit the correct filtered feed instead of getting a 400.
  if (pathname === "/search") {
    return { href: apiPath("/search") + search, title: "Search Results" };
  }

  // Check resource routes: /nodes, /nodes/:id, /services/:id, etc.
  const segments = pathname.split("/").filter(Boolean);

  const [resource, resourceId] = segments;

  if (resource && feedResources.has(resource)) {
    const title =
      segments.length === 1
        ? capitalize(resource)
        : `${capitalize(resource).replace(/s$/, "")} ${resourceId}`;

    return { href: apiPath(pathname), title };
  }

  return null;
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/**
 * Renders a <link rel="alternate"> for Atom feed autodiscovery.
 * React 19 hoists <link> elements rendered in components into <head>.
 */
export default function AtomFeedLink() {
  const { pathname, search } = useLocation();
  const feed = feedForPath(pathname, search);

  if (!feed) {
    return null;
  }

  return (
    <link
      rel="alternate"
      type="application/atom+xml"
      title={feed.title}
      href={feed.href}
    />
  );
}
