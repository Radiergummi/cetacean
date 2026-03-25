import { api } from "../api/client";
import type { SearchResult } from "../api/types";

export interface PaletteAction {
  id: string;
  label: string;
  keywords: string[];
  steps: PaletteStep[];
  execute: (...args: any[]) => Promise<void>;
  destructive?: boolean;
  /** Minimum operations level required (undefined = always available). */
  requiredLevel?: number;
}

export interface PaletteStep {
  type: "resource" | "number" | "text" | "choice";
  resourceType?: string;
  label: string;
  placeholder?: string;
  choices?: { label: string; value: string }[];
}

const removeTargets = [
  { type: "service", remove: (resource: SearchResult) => api.removeService(resource.id) },
  { type: "node", remove: (resource: SearchResult) => api.removeNode(resource.id, true) },
  { type: "stack", remove: (resource: SearchResult) => api.removeStack(resource.id) },
  { type: "config", remove: (resource: SearchResult) => api.removeConfig(resource.id) },
  { type: "secret", remove: (resource: SearchResult) => api.removeSecret(resource.id) },
  { type: "network", remove: (resource: SearchResult) => api.removeNetwork(resource.id) },
  { type: "volume", remove: (resource: SearchResult) => api.removeVolume(resource.id) },
] as const;

const removeActions: PaletteAction[] = removeTargets.map(({ type, remove }) => {
  const label = type.charAt(0).toUpperCase() + type.slice(1);

  return {
    id: `remove-${type}`,
    label: `Remove ${label}`,
    keywords: [`remove ${type}`, `delete ${type}`],
    requiredLevel: 3,
    steps: [{ type: "resource" as const, resourceType: type, label }],
    destructive: true,
    execute: async (resource: SearchResult) => {
      await remove(resource);
    },
  };
});

export function getActions(): PaletteAction[] {
  return [
    {
      id: "scale",
      label: "Scale Service",
      keywords: ["scale", "replicas"],
      requiredLevel: 1,
      steps: [
        { type: "resource", resourceType: "service", label: "Service" },
        { type: "number", label: "Replicas", placeholder: "Number of replicas" },
      ],
      execute: async (service: SearchResult, replicas: number) => {
        await api.scaleService(service.id, replicas);
      },
    },
    {
      id: "image",
      label: "Update Image",
      keywords: ["image", "deploy", "tag"],
      requiredLevel: 1,
      steps: [
        { type: "resource", resourceType: "service", label: "Service" },
        { type: "text", label: "Image", placeholder: "e.g. nginx:1.27" },
      ],
      execute: async (service: SearchResult, image: string) => {
        await api.updateServiceImage(service.id, image);
      },
    },
    {
      id: "rollback",
      label: "Rollback Service",
      keywords: ["rollback", "revert"],
      requiredLevel: 1,
      steps: [{ type: "resource", resourceType: "service", label: "Service" }],
      destructive: true,
      execute: async (service: SearchResult) => {
        await api.rollbackService(service.id);
      },
    },
    {
      id: "restart",
      label: "Restart Service",
      keywords: ["restart", "redeploy"],
      requiredLevel: 1,
      steps: [{ type: "resource", resourceType: "service", label: "Service" }],
      destructive: true,
      execute: async (service: SearchResult) => {
        await api.restartService(service.id);
      },
    },
    {
      id: "drain",
      label: "Drain Node",
      keywords: ["drain"],
      requiredLevel: 3,
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      destructive: true,
      execute: async (node: SearchResult) => {
        await api.updateNodeAvailability(node.id, "drain");
      },
    },
    {
      id: "activate",
      label: "Activate Node",
      keywords: ["activate", "undrain"],
      requiredLevel: 3,
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      execute: async (node: SearchResult) => {
        await api.updateNodeAvailability(node.id, "active");
      },
    },
    {
      id: "pause",
      label: "Pause Node",
      keywords: ["pause"],
      requiredLevel: 3,
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      destructive: true,
      execute: async (node: SearchResult) => {
        await api.updateNodeAvailability(node.id, "pause");
      },
    },
    {
      id: "remove-task",
      label: "Force Remove Task",
      keywords: ["remove task", "kill task", "kill"],
      requiredLevel: 3,
      steps: [{ type: "resource", resourceType: "task", label: "Task" }],
      destructive: true,
      execute: async (task: SearchResult) => {
        await api.removeTask(task.id);
      },
    },
    {
      id: "promote-node",
      label: "Promote Node",
      keywords: ["promote"],
      requiredLevel: 3,
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      execute: async (node: SearchResult) => {
        await api.updateNodeRole(node.id, "manager");
      },
    },
    {
      id: "demote-node",
      label: "Demote Node",
      keywords: ["demote"],
      requiredLevel: 3,
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      destructive: true,
      execute: async (node: SearchResult) => {
        await api.updateNodeRole(node.id, "worker");
      },
    },
    ...removeActions,
    {
      id: "shortcuts",
      label: "Keyboard Shortcuts",
      keywords: ["shortcuts", "hotkeys", "keys", "keyboard"],
      steps: [],
      execute: async () => {
        window.dispatchEvent(new CustomEvent("cetacean:show-shortcuts"));
      },
    },
  ];
}

export function matchAction(
  input: string,
  actions: PaletteAction[],
): { action: PaletteAction; remainder: string } | null {
  const lower = input.toLowerCase().trim();

  if (!lower) {
    return null;
  }

  for (const action of actions) {
    for (const keyword of action.keywords) {
      if (lower.startsWith(keyword)) {
        const remainder = lower.slice(keyword.length).trim();

        return { action, remainder };
      }

      if (keyword.startsWith(lower)) {
        return { action, remainder: "" };
      }
    }
  }
  return null;
}
