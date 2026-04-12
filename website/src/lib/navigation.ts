import pkg from "../../package.json" with { type: "json" };

export const repoUrl: string = pkg.repository;

export interface NavItem {
  slug: string;
  title: string;
  /** Force full page reload when navigating to this page (skips View Transitions). */
  reload?: boolean;
}

export interface NavGroup {
  label: string;
  items: NavItem[];
}

const allPages: NavItem[] = [];

export function getPrevNext(slug: string): { prev: NavItem | null; next: NavItem | null } {
  if (!allPages.length) {
    for (const group of sidebarGroups) {
      allPages.push(...group.items);
    }
  }
  const index = allPages.findIndex((item) => item.slug === slug);
  return {
    prev: index > 0 ? allPages[index - 1] : null,
    next: index >= 0 && index < allPages.length - 1 ? allPages[index + 1] : null,
  };
}

export const sidebarGroups: NavGroup[] = [
  {
    label: "Guide",
    items: [
      { slug: "getting-started", title: "Getting Started" },
      { slug: "monitoring", title: "Monitoring" },
      { slug: "authentication", title: "Authentication" },
      { slug: "authorization", title: "Authorization" },
      { slug: "dashboard", title: "Dashboard" },
      { slug: "integrations", title: "Integrations" },
      { slug: "recommendations", title: "Recommendations" },
    ],
  },
  {
    label: "Reference",
    items: [
      { slug: "configuration", title: "Configuration" },
      { slug: "api", title: "API Guide" },
      { slug: "api/explorer", title: "API Reference", reload: true },
      { slug: "api/schema", title: "Schema Reference" },
    ],
  },
];
