/**
 * Well-known error codes emitted by the Cetacean API.
 * Each entry maps an error code to a friendly message and optional action hint.
 * Error documentation is available at /api/errors/{code}.
 */

interface ErrorInfo {
  title: string;
  suggestion: string;
  action?: "force-remove" | "retry";
}

const errorDictionary: Record<string, ErrorInfo> = {
  // API: protocol / content negotiation
  API004: {
    title: "Unsupported content type",
    suggestion: "Use application/merge-patch+json or application/json-patch+json.",
  },
  API006: {
    title: "Invalid request body",
    suggestion: "Ensure the request body is well-formed JSON.",
  },

  // OPS: operations level
  OPS001: {
    title: "Operations level too low",
    suggestion:
      "Increase the CETACEAN_OPERATIONS_LEVEL environment variable and restart the server.",
  },

  // MTR: metrics / Prometheus
  MTR001: {
    title: "Prometheus not configured",
    suggestion: "Set the CETACEAN_PROMETHEUS_URL environment variable and restart the server.",
  },
  MTR002: {
    title: "Prometheus unreachable",
    suggestion: "Check that Prometheus is running and reachable at the configured URL.",
  },
  MTR005: {
    title: "Too many metrics streams",
    suggestion: "Close an existing metrics stream connection before opening a new one.",
  },

  // LOG: log streaming
  LOG001: {
    title: "Too many log streams",
    suggestion: "Close an existing log stream before opening a new one.",
  },

  // SSE: connections
  SSE001: {
    title: "Too many SSE connections",
    suggestion: "Close an existing connection before opening a new one.",
  },

  // ENG: Docker Engine
  ENG001: {
    title: "Docker Engine unavailable",
    suggestion: "Check that the Docker daemon is running and the socket is reachable.",
  },

  // SWM: swarm
  SWM001: {
    title: "Swarm API not available",
    suggestion: "Ensure Cetacean is connected to a swarm manager node.",
  },
  SWM002: {
    title: "Swarm inspect failed",
    suggestion: "The swarm may be temporarily unavailable. Try again.",
    action: "retry",
  },

  // NOD: node operations
  NOD003: {
    title: "Node not found",
    suggestion: "The node may have been removed. Refresh the page.",
  },
  NOD001: {
    title: "Node not down",
    suggestion: "Drain the node and wait for it to reach the down state, or use force removal.",
    action: "force-remove",
  },
  NOD002: {
    title: "Node version conflict",
    suggestion: "The node was modified elsewhere. Reload and retry.",
    action: "retry",
  },

  // SVC: service operations
  SVC003: {
    title: "Service not found",
    suggestion: "The service may have been removed. Refresh the page.",
  },
  SVC001: {
    title: "Service version conflict",
    suggestion: "The service was modified elsewhere. Reload and retry.",
    action: "retry",
  },
  SVC002: {
    title: "Service in use",
    suggestion: "Remove the stack that manages this service first.",
  },
  SVC005: {
    title: "Cannot scale global service",
    suggestion: "Global services run one task per node and cannot be scaled manually.",
  },
  SVC007: {
    title: "No previous spec",
    suggestion: "Rollback is only available after at least one update has been applied.",
  },

  // TSK: task operations
  TSK002: {
    title: "Task not found",
    suggestion: "The task may have been cleaned up. Refresh the page.",
  },
  TSK001: {
    title: "Task already removed",
    suggestion: "The task may have been cleaned up. Refresh the page.",
  },

  // VOL: volume operations
  VOL001: {
    title: "Volume in use",
    suggestion:
      "Stop or remove the containers using this volume first, or use force removal.",
    action: "force-remove",
  },

  // NET: network operations
  NET001: {
    title: "Network has active endpoints",
    suggestion: "Disconnect or remove the services attached to this network first.",
  },

  // CFG: config operations
  CFG001: {
    title: "Config in use",
    suggestion: "Remove the config reference from all services before deleting it.",
  },

  // SEC: secret operations
  SEC001: {
    title: "Secret in use",
    suggestion: "Remove the secret reference from all services before deleting it.",
  },
};

export function getErrorInfo(code: string | null): ErrorInfo | undefined {
  if (!code) {
    return undefined;
  }

  return errorDictionary[code];
}
