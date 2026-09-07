/**
 * The `g` chords, defined once.
 *
 * App.tsx turns each into a hotkey handler and ShortcutsHelp renders the same
 * list, so a chord cannot be registered without appearing in the `?` overlay.
 * They were two hand-written lists before, and `g m` and `g r` were live for
 * some time while the overlay claimed to show everything.
 */
export interface NavigationShortcut {
  keys: string[];
  description: string;
  path: string;
}

export const navigationShortcuts: NavigationShortcut[] = [
  { keys: ["g", "h"], description: "Go to cluster overview", path: "/" },
  { keys: ["g", "n"], description: "Go to nodes", path: "/nodes" },
  { keys: ["g", "s"], description: "Go to services", path: "/services" },
  { keys: ["g", "a"], description: "Go to tasks", path: "/tasks" },
  { keys: ["g", "k"], description: "Go to stacks", path: "/stacks" },
  { keys: ["g", "c"], description: "Go to configs", path: "/configs" },
  { keys: ["g", "x"], description: "Go to secrets", path: "/secrets" },
  { keys: ["g", "w"], description: "Go to networks", path: "/networks" },
  { keys: ["g", "v"], description: "Go to volumes", path: "/volumes" },
  { keys: ["g", "i"], description: "Go to swarm info", path: "/swarm" },
  { keys: ["g", "t"], description: "Go to topology", path: "/topology" },
  { keys: ["g", "m"], description: "Go to metrics", path: "/metrics" },
  {
    keys: ["g", "r"],
    description: "Go to recommendations",
    path: "/recommendations",
  },
];
