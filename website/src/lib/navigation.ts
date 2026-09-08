import packageJson from "../../package.json" with { type: "json" };

export const repoUrl: string = packageJson.repository;

export interface NavItem {
  slug: string;
  title: string;

  /**
   * Force full page reload when navigating to this page (skips View Transitions).
   */
  reload?: boolean;
}

export interface NavGroup {
  label: string;
  items: NavItem[];
}

const allPages: NavItem[] = [];

export function getPrevNext(slug: string): { prev: NavItem | null; next: NavItem | null } {
  if (!allPages.length) {
    for (const { items } of sidebarGroups) {
      allPages.push(...items);
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
      { slug: "api", title: "API" },
      { slug: "mcp", title: "MCP Server" },
    ],
  },
  {
    label: "Reference",
    items: [
      { slug: "configuration", title: "Configuration" },
      { slug: "mcp-tools", title: "MCP Tools" },
      { slug: "api/explorer", title: "API Reference", reload: true },
      { slug: "api/schema", title: "Schema Reference" },
      { slug: "api/errors", title: "Error Reference" },
    ],
  },
];
