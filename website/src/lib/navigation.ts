export interface NavItem {
  slug: string;
  title: string;
}

export interface NavGroup {
  label: string;
  items: NavItem[];
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
      { slug: "api", title: "API Reference" },
    ],
  },
];
