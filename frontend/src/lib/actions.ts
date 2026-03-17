import { api } from "../api/client";
import type { Node, ServiceListItem, Task } from "../api/types";

export interface PaletteAction {
  id: string;
  label: string;
  keywords: string[];
  steps: PaletteStep[];
  execute: (...args: any[]) => Promise<void>;
  destructive?: boolean;
}

export interface PaletteStep {
  type: "resource" | "number" | "text" | "choice";
  resourceType?: string;
  label: string;
  placeholder?: string;
  choices?: { label: string; value: string }[];
}

export function getActions(): PaletteAction[] {
  return [
    {
      id: "scale",
      label: "Scale Service",
      keywords: ["scale", "replicas"],
      steps: [
        { type: "resource", resourceType: "service", label: "Service" },
        { type: "number", label: "Replicas", placeholder: "Number of replicas" },
      ],
      execute: async (service: ServiceListItem, replicas: number) => {
        await api.scaleService(service.ID, replicas);
      },
    },
    {
      id: "image",
      label: "Update Image",
      keywords: ["image", "deploy", "tag"],
      steps: [
        { type: "resource", resourceType: "service", label: "Service" },
        { type: "text", label: "Image", placeholder: "e.g. nginx:1.27" },
      ],
      execute: async (service: ServiceListItem, image: string) => {
        await api.updateServiceImage(service.ID, image);
      },
    },
    {
      id: "rollback",
      label: "Rollback Service",
      keywords: ["rollback", "revert"],
      steps: [{ type: "resource", resourceType: "service", label: "Service" }],
      destructive: true,
      execute: async (service: ServiceListItem) => {
        await api.rollbackService(service.ID);
      },
    },
    {
      id: "restart",
      label: "Restart Service",
      keywords: ["restart", "redeploy"],
      steps: [{ type: "resource", resourceType: "service", label: "Service" }],
      destructive: true,
      execute: async (service: ServiceListItem) => {
        await api.restartService(service.ID);
      },
    },
    {
      id: "drain",
      label: "Drain Node",
      keywords: ["drain"],
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      destructive: true,
      execute: async (node: Node) => {
        await api.updateNodeAvailability(node.ID, "drain");
      },
    },
    {
      id: "activate",
      label: "Activate Node",
      keywords: ["activate", "undrain"],
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      execute: async (node: Node) => {
        await api.updateNodeAvailability(node.ID, "active");
      },
    },
    {
      id: "pause",
      label: "Pause Node",
      keywords: ["pause"],
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      destructive: true,
      execute: async (node: Node) => {
        await api.updateNodeAvailability(node.ID, "pause");
      },
    },
    {
      id: "remove-task",
      label: "Force Remove Task",
      keywords: ["remove", "kill", "task"],
      steps: [{ type: "resource", resourceType: "task", label: "Task" }],
      destructive: true,
      execute: async (task: Task) => {
        await api.removeTask(task.ID);
      },
    },
  ];
}

export function matchAction(
  input: string,
  actions: PaletteAction[],
): { action: PaletteAction; remainder: string } | null {
  const lower = input.toLowerCase().trim();
  if (!lower) return null;
  for (const action of actions) {
    for (const keyword of action.keywords) {
      if (lower.startsWith(keyword)) {
        const remainder = lower.slice(keyword.length).trim();
        return { action, remainder };
      }
    }
  }
  return null;
}
