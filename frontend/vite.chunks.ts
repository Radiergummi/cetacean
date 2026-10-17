/**
 * The vendor chunks the app and the website demo split their bundles into, so
 * a route preloads only what it uses.
 */
export const codeSplitting = {
  // A group also takes the dependencies of what it captures, unless a
  // higher-priority group holds them first. React and the store shim the
  // shell shares must rank first, or they land in charts or topology and
  // every route preloads those.
  groups: [
    {
      name: "vendor-react",
      test: /node_modules\/(react|react-dom|react-router|use-sync-external-store)\//,
      priority: 3,
    },
    {
      name: "vendor-charts",
      test: /node_modules\/(chart\.js|react-chartjs-2|chartjs-plugin-zoom)\//,
      priority: 2,
    },
    // ELK is loaded on demand by lib/layoutElk.ts and must stay in its
    // own chunk — grouping it with React Flow put half a megabyte in
    // front of the first rendered node.
    { name: "vendor-elk", test: /node_modules\/elkjs\//, priority: 1 },
    { name: "vendor-topology", test: /node_modules\/@xyflow\//, priority: 1 },
  ],
};
